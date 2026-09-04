package main

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

	"file-converter-api/internal/config"
	"file-converter-api/internal/github"
	"file-converter-api/internal/llm"
	"file-converter-api/internal/queue"
	"file-converter-api/internal/runner"
	"file-converter-api/internal/storage"
)

type Worker struct {
	cfg    *config.Config
	store  *storage.Storage
	q      *queue.Queue
	llm    *llm.Client
	runner *runner.Runner
	gh     *github.Client
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Worker config load failed: %v", err)
	}

	store, err := storage.New(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3UseSSL)
	if err != nil {
		log.Printf("Worker storage init warning: %v", err)
	}

	q := queue.New(cfg.RedisAddr)
	defer q.Close()

	llmClient := llm.NewClient(cfg.LLMBaseURL, cfg.LLMModel)
	cmdRunner := runner.New()
	ghClient := github.NewClient(cfg.GitHubToken, cfg.GitHubOwner, cfg.GitHubRepo)

	w := &Worker{
		cfg:    cfg,
		store:  store,
		q:      q,
		llm:    llmClient,
		runner: cmdRunner,
		gh:     ghClient,
	}

	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.RedisAddr},
		asynq.Config{
			Concurrency: 4,
			Queues: map[string]int{
				"default": 1,
			},
		},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeConversionTask, w.HandleConversionTask)

	log.Println("Starting conversion worker server...")
	if err := srv.Run(mux); err != nil {
		log.Fatalf("Worker server error: %v", err)
	}
}

func (w *Worker) HandleConversionTask(ctx context.Context, t *asynq.Task) error {
	var p queue.ConversionTaskPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("invalid payload: %w", err)
	}

	log.Printf("[Worker] Processing file: %s (%s -> %s)", p.OriginalName, p.DetectedExt, p.TargetExt)

	// 1. Analyzing Phase
	_ = w.q.PublishEvent(ctx, p.SessionUUID, queue.ProgressEvent{
		SessionUUID:  p.SessionUUID,
		FileID:       p.FileID,
		OriginalName: p.OriginalName,
		TargetExt:    p.TargetExt,
		Status:       queue.StatusAnalyzing,
		Progress:     20,
		Message:      "Analyzing conversion with LLM...",
	})

	installedTools := w.runner.ListInstalledTools()
	decision, err := w.llm.PlanConversion(ctx, p.OriginalName, p.DetectedMime, p.DetectedExt, p.TargetExt, installedTools)
	if err != nil {
		log.Printf("[Worker] LLM plan error: %v", err)
		w.publishError(ctx, &p, fmt.Sprintf("Failed to plan conversion: %v", err))
		return err
	}

	if !decision.IsConvertible {
		reason := "Conversion not supported between these formats"
		if decision.RejectionReason != nil {
			reason = *decision.RejectionReason
		}
		w.publishError(ctx, &p, reason)
		return nil
	}

	// 2. Dynamic Tool Management if missing
	toolName := decision.RequiredTool
	if !w.runner.CheckTool(toolName) {
		log.Printf("[Worker] Missing tool '%s'. Initiating dynamic installation and reporting...", toolName)

		_ = w.q.PublishEvent(ctx, p.SessionUUID, queue.ProgressEvent{
			SessionUUID:  p.SessionUUID,
			FileID:       p.FileID,
			OriginalName: p.OriginalName,
			TargetExt:    p.TargetExt,
			Status:       queue.StatusInstallingTool,
			Progress:     35,
			Message:      fmt.Sprintf("Installing required dependency (%s)...", toolName),
		})

		// Report to GitHub Issues (deduplicated)
		if w.gh != nil {
			_ = w.gh.ReportMissingDependency(ctx, toolName, p.DetectedExt, p.TargetExt)
		}

		// Attempt dynamic installation
		if err := w.runner.InstallTool(ctx, toolName); err != nil {
			log.Printf("[Worker] Failed dynamic install of %s: %v", toolName, err)
			w.publishError(ctx, &p, fmt.Sprintf("Required tool '%s' could not be dynamically installed", toolName))
			return nil
		}
	}

	// 3. Execution Phase
	_ = w.q.PublishEvent(ctx, p.SessionUUID, queue.ProgressEvent{
		SessionUUID:  p.SessionUUID,
		FileID:       p.FileID,
		OriginalName: p.OriginalName,
		TargetExt:    p.TargetExt,
		Status:       queue.StatusConverting,
		Progress:     55,
		Message:      fmt.Sprintf("Converting using %s...", toolName),
	})

	// Download input file from S3 to temp workspace
	tempDir, err := os.MkdirTemp("", "conversion-*")
	if err != nil {
		w.publishError(ctx, &p, "Failed to create worker sandbox")
		return err
	}
	defer os.RemoveAll(tempDir) // Clean up sandbox

	inputFilePath := filepath.Join(tempDir, fmt.Sprintf("input.%s", p.DetectedExt))
	outputFilePath := filepath.Join(tempDir, fmt.Sprintf("output.%s", p.TargetExt))

	s3Obj, err := w.store.Download(ctx, p.StorageKeyIn)
	if err != nil {
		w.publishError(ctx, &p, "Failed to fetch input file from storage")
		return err
	}
	defer s3Obj.Close()

	localInputFile, err := os.Create(inputFilePath)
	if err != nil {
		w.publishError(ctx, &p, "Failed to write local input file")
		return err
	}
	if _, err := io.Copy(localInputFile, s3Obj); err != nil {
		localInputFile.Close()
		w.publishError(ctx, &p, "Failed to buffer input file")
		return err
	}
	localInputFile.Close()

	// Execute sandboxed command (timeout 120s)
	logs, err := w.runner.ExecuteConversion(ctx, decision.CommandTemplate, inputFilePath, outputFilePath)
	if err != nil {
		log.Printf("[Worker] Conversion execution failed: %v", err)
		if w.gh != nil {
			_ = w.gh.ReportConversionFailure(ctx, toolName, p.DetectedExt, p.TargetExt, decision.CommandTemplate, logs)
		}
		w.publishError(ctx, &p, fmt.Sprintf("Conversion failed: %v", err))
		return nil
	}

	// In case output is a directory (e.g. pdftoppm or libreoffice outputs with different names)
	finalOutputFile := outputFilePath
	if _, err := os.Stat(finalOutputFile); os.IsNotExist(err) {
		// Check if any matching target extension file was created in tempDir
		files, _ := os.ReadDir(tempDir)
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(strings.ToLower(f.Name()), "."+p.TargetExt) {
				finalOutputFile = filepath.Join(tempDir, f.Name())
				break
			}
		}
	}

	outputStat, err := os.Stat(finalOutputFile)
	if err != nil {
		w.publishError(ctx, &p, "Converted output file not generated by tool")
		return nil
	}

	// 4. Upload converted file to SeaweedFS
	outFileReader, err := os.Open(finalOutputFile)
	if err != nil {
		w.publishError(ctx, &p, "Failed to open converted output file")
		return err
	}
	defer outFileReader.Close()

	targetMime := mime.TypeByExtension("." + p.TargetExt)
	if targetMime == "" {
		targetMime = "application/octet-stream"
	}

	if err := w.store.Upload(ctx, p.StorageKeyOut, outFileReader, outputStat.Size(), targetMime); err != nil {
		w.publishError(ctx, &p, "Failed to persist converted file to storage")
		return err
	}

	// Store metadata in Redis for download lookup (TTL 2 hours)
	downloadName := fmt.Sprintf("%s.%s", strings.TrimSuffix(p.OriginalName, filepath.Ext(p.OriginalName)), p.TargetExt)
	metaData, _ := json.Marshal(map[string]string{
		"file_id":         p.FileID,
		"original_name":   p.OriginalName,
		"download_name":   downloadName,
		"storage_key_out": p.StorageKeyOut,
		"target_mime":     targetMime,
	})
	metaKey := fmt.Sprintf("file_meta:%s", p.FileID)
	w.q.GetRedisClient().Set(ctx, metaKey, metaData, 2*time.Hour)

	// 5. Publish Ready Event
	downloadURL := fmt.Sprintf("/api/download/%s", p.FileID)
	_ = w.q.PublishEvent(ctx, p.SessionUUID, queue.ProgressEvent{
		SessionUUID:  p.SessionUUID,
		FileID:       p.FileID,
		OriginalName: p.OriginalName,
		TargetExt:    p.TargetExt,
		Status:       queue.StatusReady,
		Progress:     100,
		Message:      "Conversion completed successfully",
		DownloadURL:  downloadURL,
	})

	log.Printf("[Worker] Successfully finished %s -> %s", p.OriginalName, downloadName)
	return nil
}

func (w *Worker) publishError(ctx context.Context, p *queue.ConversionTaskPayload, errMsg string) {
	_ = w.q.PublishEvent(ctx, p.SessionUUID, queue.ProgressEvent{
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
