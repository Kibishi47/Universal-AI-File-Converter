package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

const (
	TypeConversionTask = "task:convert_file"
)

type ConversionTaskPayload struct {
	SessionUUID   string `json:"session_uuid"`
	FileID        string `json:"file_id"`
	OriginalName  string `json:"original_name"`
	StorageKeyIn  string `json:"storage_key_in"`
	DetectedMime  string `json:"detected_mime"`
	DetectedExt   string `json:"detected_ext"`
	TargetExt     string `json:"target_ext"`
	TargetMime    string `json:"target_mime"`
	StorageKeyOut string `json:"storage_key_out"`
}

type EventStatus string

const (
	StatusQueued         EventStatus = "queued"
	StatusAnalyzing      EventStatus = "analyzing"
	StatusInstallingTool EventStatus = "installing_tool"
	StatusConverting     EventStatus = "converting"
	StatusReady          EventStatus = "ready"
	StatusError          EventStatus = "error"
)

type ProgressEvent struct {
	SessionUUID string      `json:"session_uuid"`
	FileID      string      `json:"file_id"`
	OriginalName string     `json:"original_name"`
	TargetExt   string      `json:"target_ext"`
	Status      EventStatus `json:"status"`
	Progress    int         `json:"progress"` // 0 to 100
	Message     string      `json:"message"`
	DownloadURL string      `json:"download_url,omitempty"`
	Error       string      `json:"error,omitempty"`
	Timestamp   int64       `json:"timestamp"`
}

type Queue struct {
	client      *asynq.Client
	redisClient *redis.Client
}

func New(redisAddr string) *Queue {
	redisOpt := asynq.RedisClientOpt{Addr: redisAddr}
	client := asynq.NewClient(redisOpt)

	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	return &Queue{
		client:      client,
		redisClient: rdb,
	}
}

func (q *Queue) Close() {
	_ = q.client.Close()
	_ = q.redisClient.Close()
}

func (q *Queue) EnqueueConversion(payload *ConversionTaskPayload) (*asynq.TaskInfo, error) {
	bytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal task payload: %w", err)
	}

	task := asynq.NewTask(TypeConversionTask, bytes, asynq.MaxRetry(3), asynq.Timeout(5*time.Minute))
	return q.client.Enqueue(task)
}

// PublishEvent sends a real-time progress update to the session channel
func (q *Queue) PublishEvent(ctx context.Context, sessionUUID string, event ProgressEvent) error {
	event.Timestamp = time.Now().UnixMilli()
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	channel := fmt.Sprintf("session:%s", sessionUUID)
	return q.redisClient.Publish(ctx, channel, payload).Err()
}

// Subscribe returns a Redis pubsub channel for a session
func (q *Queue) Subscribe(ctx context.Context, sessionUUID string) *redis.PubSub {
	channel := fmt.Sprintf("session:%s", sessionUUID)
	return q.redisClient.Subscribe(ctx, channel)
}

func (q *Queue) GetRedisClient() *redis.Client {
	return q.redisClient
}
