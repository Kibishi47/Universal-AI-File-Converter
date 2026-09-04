package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port           string
	RedisAddr      string
	S3Endpoint     string
	S3Bucket       string
	S3AccessKey    string
	S3SecretKey    string
	S3UseSSL       bool
	LLMBaseURL     string
	LLMModel       string
	GitHubToken    string
	GitHubOwner    string
	GitHubRepo     string
	MaxUploadSizeMB int64
}

func Load() (*Config, error) {
	port := getEnv("PORT", "8080")
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	s3Endpoint := getEnv("S3_ENDPOINT", "localhost:8333")
	s3Bucket := getEnv("S3_BUCKET", "conversions")
	s3AccessKey := getEnv("S3_ACCESS_KEY", "anykey")
	s3SecretKey := getEnv("S3_SECRET_KEY", "anysecret")
	s3UseSSL, _ := strconv.ParseBool(getEnv("S3_USE_SSL", "false"))

	llmBaseURL := getEnv("LLM_BASE_URL", "http://localhost:8080")
	llmModel := getEnv("LLM_MODEL", "default")

	githubToken := getEnv("GITHUB_TOKEN", "")
	githubOwner := getEnv("GITHUB_OWNER", "")
	githubRepo := getEnv("GITHUB_REPO", "")

	maxUploadMB, err := strconv.ParseInt(getEnv("MAX_UPLOAD_SIZE_MB", "100"), 10, 64)
	if err != nil {
		maxUploadMB = 100
	}

	cfg := &Config{
		Port:            port,
		RedisAddr:       redisAddr,
		S3Endpoint:      s3Endpoint,
		S3Bucket:        s3Bucket,
		S3AccessKey:     s3AccessKey,
		S3SecretKey:     s3SecretKey,
		S3UseSSL:        s3UseSSL,
		LLMBaseURL:      llmBaseURL,
		LLMModel:        llmModel,
		GitHubToken:     githubToken,
		GitHubOwner:     githubOwner,
		GitHubRepo:      githubRepo,
		MaxUploadSizeMB: maxUploadMB,
	}

	return cfg, nil
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func (c *Config) Validate() error {
	if c.Port == "" {
		return fmt.Errorf("PORT cannot be empty")
	}
	if c.RedisAddr == "" {
		return fmt.Errorf("REDIS_ADDR cannot be empty")
	}
	return nil
}
