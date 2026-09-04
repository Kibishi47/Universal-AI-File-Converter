package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Storage struct {
	client     *minio.Client
	bucketName string
}

func New(endpoint, accessKey, secretKey, bucketName string, useSSL bool) (*Storage, error) {
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init minio client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Ensure bucket exists
	exists, err := minioClient.BucketExists(ctx, bucketName)
	if err != nil {
		// Log or ignore if connection fails on startup, but try to create if accessible
		_ = minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{})
	} else if !exists {
		if err := minioClient.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("failed to create bucket %s: %w", bucketName, err)
		}
	}

	return &Storage{
		client:     minioClient,
		bucketName: bucketName,
	}, nil
}

// Upload uploads an object with specified size, contentType and custom metadata (e.g. TTL expiry time)
func (s *Storage) Upload(ctx context.Context, objectName string, reader io.Reader, size int64, contentType string) error {
	userMetadata := map[string]string{
		"uploaded_at": time.Now().UTC().Format(time.RFC3339),
		"expires_at":  time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339),
	}

	_, err := s.client.PutObject(ctx, s.bucketName, objectName, reader, size, minio.PutObjectOptions{
		ContentType:  contentType,
		UserMetadata: userMetadata,
	})
	if err != nil {
		return fmt.Errorf("failed to put object %s: %w", objectName, err)
	}
	return nil
}

// Download returns a reader for the requested object
func (s *Storage) Download(ctx context.Context, objectName string) (*minio.Object, error) {
	obj, err := s.client.GetObject(ctx, s.bucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get object %s: %w", objectName, err)
	}
	return obj, nil
}

// StatObject returns object info
func (s *Storage) StatObject(ctx context.Context, objectName string) (minio.ObjectInfo, error) {
	return s.client.StatObject(ctx, s.bucketName, objectName, minio.StatObjectOptions{})
}

// Delete removes an object
func (s *Storage) Delete(ctx context.Context, objectName string) error {
	return s.client.RemoveObject(ctx, s.bucketName, objectName, minio.RemoveObjectOptions{})
}

// PresignedURL generates temporary download URL
func (s *Storage) PresignedURL(ctx context.Context, objectName string, expires time.Duration) (*url.URL, error) {
	return s.client.PresignedGetObject(ctx, s.bucketName, objectName, expires, nil)
}
