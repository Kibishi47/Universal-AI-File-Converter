package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"

	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/config"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/detector"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/queue"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/storage"
	"github.com/Kibishi47/Universal-AI-File-Converter/apps/api/internal/zipper"
)

type Server struct {
	cfg   *config.Config
	store *storage.Storage
	queue *queue.Queue
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	store, err := storage.New(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3UseSSL)
	if err != nil {
		log.Printf("Warning: storage connection error: %v", err)
	}

	q, err := queue.New(cfg.RedisAddr)
	if err != nil {
		log.Fatalf("Failed to initialize queue: %v", err)
	}
	defer q.Close()

	srv := &Server{
		cfg:   cfg,
		store: store,
		queue: q,
	}

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(120 * time.Second))

	// CORS configuration for privacy & local Nuxt development
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link", "Content-Disposition"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	r.Route("/api", func(api chi.Router) {
		api.Post("/upload", srv.handleUpload)
		api.Get("/events/{session_uuid}", srv.handleEvents)
		api.Get("/download/{file_id}", srv.handleDownload)
		api.Get("/download/zip", srv.handleDownloadZip)
	})

	addr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("HTTP Server listening on %s", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("HTTP server error: %v", err)
	}
}

type UploadResponse struct {
	FileID       string `json:"file_id"`
	OriginalName string `json:"original_name"`
	DetectedMime string `json:"detected_mime"`
	DetectedExt  string `json:"detected_ext"`
	TargetExt    string `json:"target_ext"`
	Status       string `json:"status"`
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	maxBytes := s.cfg.MaxUploadSizeMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(maxBytes); err != nil {
		http.Error(w, fmt.Sprintf("File too large or invalid multipart form: %v", err), http.StatusBadRequest)
		return
	}

	sessionUUID := r.FormValue("session_uuid")
	if sessionUUID == "" {
		sessionUUID = uuid.New().String()
	}

	targetExt := strings.ToLower(strings.TrimSpace(r.FormValue("target_ext")))
	if targetExt == "" {
		targetExt = "pdf" // Default target format if unspecified
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf("Missing file in form field 'file': %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// 1. Detect format using magic bytes
	detectedInfo, multiReader, err := detector.Detect(file, header.Filename)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to inspect file: %v", err), http.StatusInternalServerError)
		return
	}

	fileID := uuid.New().String()
	storageKeyIn := fmt.Sprintf("uploads/%s/%s-%s", sessionUUID, fileID, filepath.Base(header.Filename))
	storageKeyOut := fmt.Sprintf("converted/%s/%s.%s", sessionUUID, fileID, targetExt)

	// 2. Upload to SeaweedFS (S3)
	if s.store != nil {
		if err := s.store.Upload(r.Context(), storageKeyIn, multiReader, header.Size, detectedInfo.MIME); err != nil {
			http.Error(w, fmt.Sprintf("Failed to store file: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// 3. Enqueue conversion job to Redis Asynq
	taskPayload := &queue.ConversionTaskPayload{
		SessionUUID:   sessionUUID,
		FileID:        fileID,
		OriginalName:  header.Filename,
		StorageKeyIn:  storageKeyIn,
		DetectedMime:  detectedInfo.MIME,
		DetectedExt:   detectedInfo.Extension,
		TargetExt:     targetExt,
		StorageKeyOut: storageKeyOut,
	}

	if _, err := s.queue.EnqueueConversion(taskPayload); err != nil {
		http.Error(w, fmt.Sprintf("Failed to enqueue conversion: %v", err), http.StatusInternalServerError)
		return
	}

	// 4. Save metadata in Redis for session tracking & zip export
	sessionFileKey := fmt.Sprintf("session_files:%s", sessionUUID)
	sessionFileData, _ := json.Marshal(map[string]string{
		"file_id":         fileID,
		"original_name":   header.Filename,
		"target_ext":      targetExt,
		"storage_key_out": storageKeyOut,
		"download_name":   fmt.Sprintf("%s.%s", strings.TrimSuffix(header.Filename, filepath.Ext(header.Filename)), targetExt),
	})
	s.queue.GetRedisClient().SAdd(r.Context(), sessionFileKey, sessionFileData)

	// Broadcast initial queued event
	_ = s.queue.PublishEvent(r.Context(), sessionUUID, queue.ProgressEvent{
		SessionUUID:  sessionUUID,
		FileID:       fileID,
		OriginalName: header.Filename,
		TargetExt:    targetExt,
		Status:       queue.StatusQueued,
		Progress:     0,
		Message:      "Enqueued for conversion",
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(UploadResponse{
		FileID:       fileID,
		OriginalName: header.Filename,
		DetectedMime: detectedInfo.MIME,
		DetectedExt:  detectedInfo.Extension,
		TargetExt:    targetExt,
		Status:       string(queue.StatusQueued),
	})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	sessionUUID := chi.URLParam(r, "session_uuid")
	if sessionUUID == "" {
		http.Error(w, "session_uuid parameter required", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send initial SSE connection message
	fmt.Fprintf(w, "event: connected\ndata: {\"session_uuid\":\"%s\"}\n\n", sessionUUID)
	flusher.Flush()

	pubsub := s.queue.Subscribe(r.Context(), sessionUUID)
	defer pubsub.Close()

	ch := pubsub.Channel()
	ticker := time.NewTicker(20 * time.Second) // Keepalive heartbeat
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", msg.Payload)
			flusher.Flush()
		}
	}
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	fileID := chi.URLParam(r, "file_id")
	if fileID == "" {
		http.Error(w, "file_id required", http.StatusBadRequest)
		return
	}

	// Retrieve file metadata from Redis
	metaKey := fmt.Sprintf("file_meta:%s", fileID)
	data, err := s.queue.GetRedisClient().Get(r.Context(), metaKey).Result()
	if err != nil {
		http.Error(w, "File not found or expired", http.StatusNotFound)
		return
	}

	var meta struct {
		StorageKeyOut string `json:"storage_key_out"`
		DownloadName  string `json:"download_name"`
		TargetMime    string `json:"target_mime"`
	}
	if err := json.Unmarshal([]byte(data), &meta); err != nil {
		http.Error(w, "Invalid file metadata", http.StatusInternalServerError)
		return
	}

	obj, err := s.store.Download(r.Context(), meta.StorageKeyOut)
	if err != nil {
		http.Error(w, "File not available on storage", http.StatusNotFound)
		return
	}
	defer obj.Close()

	stat, err := obj.Stat()
	if err != nil {
		http.Error(w, "Unable to inspect stored file", http.StatusInternalServerError)
		return
	}

	contentType := meta.TargetMime
	if contentType == "" {
		contentType = stat.ContentType
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", meta.DownloadName))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size))

	_, _ = io.Copy(w, obj)
}

func (s *Server) handleDownloadZip(w http.ResponseWriter, r *http.Request) {
	sessionUUID := r.URL.Query().Get("session")
	if sessionUUID == "" {
		http.Error(w, "session query parameter required", http.StatusBadRequest)
		return
	}

	sessionFileKey := fmt.Sprintf("session_files:%s", sessionUUID)
	rawEntries, err := s.queue.GetRedisClient().SMembers(r.Context(), sessionFileKey).Result()
	if err != nil || len(rawEntries) == 0 {
		http.Error(w, "No files found for this session", http.StatusNotFound)
		return
	}

	var zipEntries []zipper.FileEntry
	for _, raw := range rawEntries {
		var item struct {
			DownloadName  string `json:"download_name"`
			StorageKeyOut string `json:"storage_key_out"`
		}
		if err := json.Unmarshal([]byte(raw), &item); err == nil {
			// Verify file exists on storage
			if _, err := s.store.StatObject(r.Context(), item.StorageKeyOut); err == nil {
				zipEntries = append(zipEntries, zipper.FileEntry{
					FileName:   item.DownloadName,
					StorageKey: item.StorageKeyOut,
				})
			}
		}
	}

	if len(zipEntries) == 0 {
		http.Error(w, "No converted files ready for download yet", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"converted_files_%s.zip\"", sessionUUID[:8]))

	if err := zipper.StreamZip(r.Context(), s.store, zipEntries, w); err != nil {
		log.Printf("Error streaming zip: %v", err)
	}
}
