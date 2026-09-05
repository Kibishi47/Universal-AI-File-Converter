package task

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hibiken/asynq"

	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/config"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/github"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/llm"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/queue"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/runner"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/storage"
)

type ConversionHandler struct {
	cfg    *config.Config
	store  *storage.Storage
	q      *queue.Queue
	llm    *llm.Client
	runner *runner.Runner
	gh     *github.Client
}

func NewConversionHandler(
	cfg *config.Config,
	store *storage.Storage,
	q *queue.Queue,
	llmClient *llm.Client,
	cmdRunner *runner.Runner,
	ghClient *github.Client,
) *ConversionHandler {
	return &ConversionHandler{
		cfg:    cfg,
		store:  store,
		q:      q,
		llm:    llmClient,
		runner: cmdRunner,
		gh:     ghClient,
	}
}

// Handle processes the interleaved agentic conversion loop
func (h *ConversionHandler) Handle(ctx context.Context, t *asynq.Task) error {
	var p queue.ConversionTaskPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("invalid conversion task payload: %w", err)
	}

	log.Printf("[Worker Task] Interleaving ReAct loop started: %s (%s -> %s)", p.OriginalName, p.DetectedExt, p.TargetExt)

	// ==========================================
	// Phase 1 : Feasibility Check (GrammarFeasibility)
	// ==========================================
	_ = h.q.PublishEvent(ctx, p.SessionUUID, queue.ProgressEvent{
		SessionUUID:  p.SessionUUID,
		FileID:       p.FileID,
		OriginalName: p.OriginalName,
		TargetExt:    p.TargetExt,
		Status:       queue.StatusAnalyzing,
		Progress:     15,
		Message:      "Vérification de faisabilité par l'IA...",
	})

	statIn, statErr := h.store.StatObject(ctx, p.StorageKeyIn)
	var fileSize int64 = 0
	if statErr == nil {
		fileSize = statIn.Size
	}

	feasibility, err := h.llm.CheckFeasibility(ctx, p.OriginalName, fileSize, p.DetectedMime, p.DetectedExt, p.TargetExt)
	if err != nil {
		log.Printf("[Worker Task] Feasibility check error: %v, continuing with fallback", err)
	} else if !feasibility.Convertible {
		// Stop immediately without heating CPU
		reason := "La conversion entre ces formats n'est pas possible."
		if feasibility.Reason != "" {
			reason = feasibility.Reason
		}
		log.Printf("[Worker Task] Incompatible formats rejected: %s", reason)
		h.publishError(ctx, &p, reason)
		return nil
	}

	// ==========================================
	// Phase 2 : Tool Discovery & Probing (GrammarTools)
	// ==========================================
	_ = h.q.PublishEvent(ctx, p.SessionUUID, queue.ProgressEvent{
		SessionUUID:  p.SessionUUID,
		FileID:       p.FileID,
		OriginalName: p.OriginalName,
		TargetExt:    p.TargetExt,
		Status:       queue.StatusAnalyzing,
		Progress:     30,
		Message:      "Découverte des outils et inspection de l'environnement...",
	})

	toolsResult, err := h.llm.DiscoverTools(ctx, p.DetectedExt, p.TargetExt)
	if err != nil {
		log.Printf("[Worker Task] Tool discovery warning: %v", err)
	}

	var candidateTools []string
	if toolsResult != nil {
		candidateTools = append(candidateTools, toolsResult.RequiredTools...)
		candidateTools = append(candidateTools, toolsResult.Alternatives...)
	}

	// Check local presence with exec.LookPath
	var availableTools []string
	var missingTools []string

	for _, tName := range candidateTools {
		if h.runner.CheckTool(tName) {
			availableTools = append(availableTools, tName)
		} else {
			missingTools = append(missingTools, tName)
		}
	}

	// If tools are missing, attempt dynamic installation & report to GitHub
	for _, missingTool := range missingTools {
		log.Printf("[Worker Task] Missing tool detected: %s", missingTool)
		_ = h.q.PublishEvent(ctx, p.SessionUUID, queue.ProgressEvent{
			SessionUUID:  p.SessionUUID,
			FileID:       p.FileID,
			OriginalName: p.OriginalName,
			TargetExt:    p.TargetExt,
			Status:       queue.StatusInstallingTool,
			Progress:     45,
			Message:      fmt.Sprintf("Préparation de l'outil requis (%s)...", missingTool),
		})

		// Report deduplicated missing tool to GitHub
		if h.gh != nil {
			_ = h.gh.ReportMissingDependency(ctx, missingTool, p.DetectedExt, p.TargetExt)
		}

		// Attempt dynamic installation
		if installErr := h.runner.InstallTool(ctx, missingTool); installErr == nil {
			log.Printf("[Worker Task] Dynamic installation succeeded for %s", missingTool)
			availableTools = append(availableTools, missingTool)
		} else {
			log.Printf("[Worker Task] Dynamic installation failed for %s: %v", missingTool, installErr)
		}
	}

	// Also append any preinstalled system tools
	availableTools = append(availableTools, h.runner.ListInstalledTools()...)

	// ==========================================
	// Phase 3 & 4 : Plan Synthesis & Agentic Self-Correction Loop
	// ==========================================
	_ = h.q.PublishEvent(ctx, p.SessionUUID, queue.ProgressEvent{
		SessionUUID:  p.SessionUUID,
		FileID:       p.FileID,
		OriginalName: p.OriginalName,
		TargetExt:    p.TargetExt,
		Status:       queue.StatusConverting,
		Progress:     60,
		Message:      "Génération du plan d'exécution et conversion...",
	})

	// Setup isolated sandbox directory: /tmp/converter/<session_uuid>/<file_id>/
	sandboxDir := filepath.Join(os.TempDir(), "converter", p.SessionUUID, p.FileID)
	if err := os.MkdirAll(sandboxDir, 0755); err != nil {
		h.publishError(ctx, &p, "Impossible d'initialiser le bac à sable de conversion.")
		return err
	}
	defer os.RemoveAll(sandboxDir)

	inputFilePath := filepath.Join(sandboxDir, fmt.Sprintf("input.%s", p.DetectedExt))
	outputFilePath := filepath.Join(sandboxDir, fmt.Sprintf("output.%s", p.TargetExt))

	// Fetch input file from storage
	s3Obj, err := h.store.Download(ctx, p.StorageKeyIn)
	if err != nil {
		h.publishError(ctx, &p, "Fichier source introuvable sur le stockage.")
		return err
	}
	defer s3Obj.Close()

	localInputFile, err := os.Create(inputFilePath)
	if err != nil {
		h.publishError(ctx, &p, "Erreur d'écriture dans le bac à sable.")
		return err
	}
	if _, err := io.Copy(localInputFile, s3Obj); err != nil {
		localInputFile.Close()
		h.publishError(ctx, &p, "Erreur de transfert du fichier source.")
		return err
	}
	localInputFile.Close()

	// ReAct execution loop with up to 2 self-correction retries
	const maxRetries = 2
	var lastStderr string
	var executionSuccess bool

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("[Worker Task] Self-correction attempt %d for file %s", attempt, p.OriginalName)
			_ = h.q.PublishEvent(ctx, p.SessionUUID, queue.ProgressEvent{
				SessionUUID:  p.SessionUUID,
				FileID:       p.FileID,
				OriginalName: p.OriginalName,
				TargetExt:    p.TargetExt,
				Status:       queue.StatusConverting,
				Progress:     60 + (attempt * 10),
				Message:      fmt.Sprintf("Auto-correction agentique en cours (essai %d/%d)...", attempt, maxRetries),
			})
		}

		plan, err := h.llm.SynthesizePlan(ctx, p.DetectedExt, p.TargetExt, availableTools, lastStderr)
		if err != nil {
			log.Printf("[Worker Task] Plan synthesis error: %v", err)
			lastStderr = fmt.Sprintf("plan synthesis failed: %v", err)
			continue
		}

		if plan == nil || len(plan.Steps) == 0 {
			lastStderr = "empty execution steps generated by llm"
			continue
		}

		stepFailed := false
		for stepIdx, step := range plan.Steps {
			// Replace $INPUT and $OUTPUT tokens securely
			var secureArgs []string
			for _, arg := range step.Args {
				a := strings.ReplaceAll(arg, "$INPUT", inputFilePath)
				a = strings.ReplaceAll(a, "$OUTPUT", outputFilePath)
				secureArgs = append(secureArgs, a)
			}

			log.Printf("[Worker Task] Executing step %d: %s %v", stepIdx+1, step.Command, secureArgs)
			_, stderrStr, execErr := h.runner.ExecuteStep(ctx, step.Command, secureArgs, sandboxDir)
			if execErr != nil {
				log.Printf("[Worker Task] Step %s failed: %v, stderr: %s", step.Command, execErr, stderrStr)
				lastStderr = fmt.Sprintf("Command %s failed: %v. Stderr: %s", step.Command, execErr, stderrStr)
				stepFailed = true
				break
			}
		}

		if !stepFailed {
			executionSuccess = true
			break
		}
	}

	if !executionSuccess {
		log.Printf("[Worker Task] All execution attempts failed: %s", lastStderr)
		if h.gh != nil {
			_ = h.gh.ReportConversionFailure(ctx, "agentic-pipeline", p.DetectedExt, p.TargetExt, "multi-step", lastStderr)
		}
		h.publishError(ctx, &p, "La conversion a échoué après plusieurs tentatives d'auto-correction.")
		return nil
	}

	// Resolve output file (check direct output or any generated target format in sandbox)
	finalOutputFile := outputFilePath
	if _, err := os.Stat(finalOutputFile); os.IsNotExist(err) {
		entries, _ := os.ReadDir(sandboxDir)
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), "."+p.TargetExt) {
				finalOutputFile = filepath.Join(sandboxDir, e.Name())
				break
			}
		}
	}

	outputStat, err := os.Stat(finalOutputFile)
	if err != nil {
		h.publishError(ctx, &p, "Le fichier converti n'a pas été généré.")
		return nil
	}

	// Persist converted file to SeaweedFS
	outFileReader, err := os.Open(finalOutputFile)
	if err != nil {
		h.publishError(ctx, &p, "Impossible d'accéder au fichier converti généré.")
		return err
	}
	defer outFileReader.Close()

	targetMime := mime.TypeByExtension("." + p.TargetExt)
	if targetMime == "" {
		targetMime = "application/octet-stream"
	}

	if err := h.store.Upload(ctx, p.StorageKeyOut, outFileReader, outputStat.Size(), targetMime); err != nil {
		h.publishError(ctx, &p, "Échec du stockage du fichier converti.")
		return err
	}

	// Store metadata in Redis for download lookup (TTL 24 hours)
	downloadName := fmt.Sprintf("%s.%s", strings.TrimSuffix(p.OriginalName, filepath.Ext(p.OriginalName)), p.TargetExt)
	metaData, _ := json.Marshal(map[string]string{
		"file_id":         p.FileID,
		"original_name":   p.OriginalName,
		"download_name":   downloadName,
		"storage_key_out": p.StorageKeyOut,
		"target_mime":     targetMime,
	})
	metaKey := fmt.Sprintf("file_meta:%s", p.FileID)
	h.q.GetRedisClient().Set(ctx, metaKey, metaData, 24*time.Hour)

	// Emit ready SSE event
	downloadURL := fmt.Sprintf("/api/download/%s", p.FileID)
	_ = h.q.PublishEvent(ctx, p.SessionUUID, queue.ProgressEvent{
		SessionUUID:  p.SessionUUID,
		FileID:       p.FileID,
		OriginalName: p.OriginalName,
		TargetExt:    p.TargetExt,
		Status:       queue.StatusReady,
		Progress:     100,
		Message:      "Conversion terminée avec succès",
		DownloadURL:  downloadURL,
	})

	log.Printf("[Worker Task] Conversion successfully completed: %s -> %s", p.OriginalName, downloadName)
	return nil
}

func (h *ConversionHandler) publishError(ctx context.Context, p *queue.ConversionTaskPayload, errMsg string) {
	_ = h.q.PublishEvent(ctx, p.SessionUUID, queue.ProgressEvent{
		SessionUUID:  p.SessionUUID,
		FileID:       p.FileID,
		OriginalName: p.OriginalName,
		TargetExt:    p.TargetExt,
		Status:       queue.StatusError,
		Progress:     100,
		Message:      errMsg,
		Error:        errMsg,
	})
}
