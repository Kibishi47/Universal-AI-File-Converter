package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/hibiken/asynq"

	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/config"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/github"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/llm"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/queue"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/runner"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/storage"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/task"
)

type Worker struct {
	cfg     *config.Config
	store   *storage.Storage
	q       *queue.Queue
	handler *task.ConversionHandler
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

	q, err := queue.New(cfg.RedisAddr)
	if err != nil {
		log.Fatalf("Failed to initialize queue in worker: %v", err)
	}
	defer q.Close()

	llmClient := llm.NewClient(cfg.LLMBaseURL, cfg.LLMModel)
	cmdRunner := runner.New()
	ghClient := github.NewClient(cfg.GitHubToken, cfg.GitHubOwner, cfg.GitHubRepo)

	conversionHandler := task.NewConversionHandler(cfg, store, q, llmClient, cmdRunner, ghClient)

	// Launch periodic 24-hour cleanup routine (runs every 1 hour)
	go startCleanupRoutine(store)

	w := &Worker{
		cfg:     cfg,
		store:   store,
		q:       q,
		handler: conversionHandler,
	}

	redisConnOpt, err := queue.ParseRedisConnOpt(cfg.RedisAddr)
	if err != nil {
		log.Fatalf("Failed to parse Redis configuration for worker server: %v", err)
	}

	srv := asynq.NewServer(
		redisConnOpt,
		asynq.Config{
			Concurrency: 1,
			Queues: map[string]int{
				"default": 1,
			},
		},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(queue.TypeConversionTask, w.handler.Handle)

	log.Println("Starting conversion worker server...")
	if err := srv.Run(mux); err != nil {
		log.Fatalf("Worker server error: %v", err)
	}
}

// startCleanupRoutine runs every 1 hour and removes temporary sandbox folders and S3 files older than 24h
func startCleanupRoutine(store *storage.Storage) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for range ticker.C {
		log.Println("[Cleanup Routine] Starting scheduled 24-hour TTL cleanup...")

		// 1. Clean local temporary directories older than 24h
		baseTemp := filepath.Join(os.TempDir(), "converter")
		if entries, err := os.ReadDir(baseTemp); err == nil {
			cutoff := time.Now().Add(-24 * time.Hour)
			for _, entry := range entries {
				fullPath := filepath.Join(baseTemp, entry.Name())
				if info, statErr := entry.Info(); statErr == nil {
					if info.ModTime().Before(cutoff) {
						_ = os.RemoveAll(fullPath)
						log.Printf("[Cleanup Routine] Removed expired local directory: %s", fullPath)
					}
				}
			}
		}

		// 2. Clean expired S3 storage objects (> 24 hours)
		if store != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			count, err := store.DeleteExpiredObjects(ctx, 24*time.Hour)
			cancel()
			if err != nil {
				log.Printf("[Cleanup Routine] S3 cleanup error: %v", err)
			} else {
				log.Printf("[Cleanup Routine] Removed %d expired objects from S3 storage", count)
			}
		}
	}
}


