package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// ParseRedisConnOpt parses a redis address string (supporting redis://, rediss:// or host:port) into an asynq.RedisConnOpt
func ParseRedisConnOpt(redisAddr string) (asynq.RedisConnOpt, error) {
	if strings.HasPrefix(redisAddr, "redis://") || strings.HasPrefix(redisAddr, "rediss://") {
		return asynq.ParseRedisURI(redisAddr)
	}
	return asynq.RedisClientOpt{Addr: redisAddr}, nil
}

// New creates a Queue instance, properly handling redis://, rediss:// or raw host:port strings
func New(redisAddr string) (*Queue, error) {
	var asynqOpt asynq.RedisConnOpt
	var rdb *redis.Client

	if strings.HasPrefix(redisAddr, "redis://") || strings.HasPrefix(redisAddr, "rediss://") {
		var err error
		asynqOpt, err = asynq.ParseRedisURI(redisAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse asynq redis URI %q: %w", redisAddr, err)
		}

		redisOpts, err := redis.ParseURL(redisAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse redis URL %q: %w", redisAddr, err)
		}
		rdb = redis.NewClient(redisOpts)
	} else {
		asynqOpt = asynq.RedisClientOpt{Addr: redisAddr}
		rdb = redis.NewClient(&redis.Options{
			Addr: redisAddr,
		})
	}

	client := asynq.NewClient(asynqOpt)

	return &Queue{
		client:      client,
		redisClient: rdb,
	}, nil
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
