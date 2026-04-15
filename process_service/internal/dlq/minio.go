package dlq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Writer interface {
	SendInvalidPayload(ctx context.Context, messageID string, payload []byte, reason string) error
}

type Record struct {
	MessageID string `json:"message_id"`
	Reason    string `json:"reason"`
	Payload   string `json:"payload,omitempty"`
	CreatedAt string `json:"created_at"`
}

type MinIO struct {
	client *minio.Client
	bucket string
}

func NewMinIOFromEnv() (*MinIO, error) {
	endpoint := os.Getenv("MINIO_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:9000"
	}

	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "minioadmin"
	}

	secretKey := os.Getenv("MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = "minioadmin"
	}

	bucket := os.Getenv("MINIO_DLQ_BUCKET")
	if bucket == "" {
		bucket = "tasks-dlq"
	}

	useSSL := false
	if rawSSL := os.Getenv("MINIO_USE_SSL"); rawSSL != "" {
		parsed, err := strconv.ParseBool(rawSSL)
		if err == nil {
			useSSL = parsed
		}
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, err
	}

	return &MinIO{client: client, bucket: bucket}, nil
}

func (m *MinIO) SendInvalidPayload(ctx context.Context, messageID string, payload []byte, reason string) error {
	if m == nil || m.client == nil {
		return fmt.Errorf("dlq storage is not configured")
	}

	if err := m.ensureBucket(ctx); err != nil {
		return err
	}

	rec := Record{
		MessageID: messageID,
		Reason:    reason,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if payload != nil {
		rec.Payload = string(payload)
	}

	body, err := json.Marshal(rec)
	if err != nil {
		return err
	}

	objectName := fmt.Sprintf("invalid/%d-%s.json", time.Now().UnixNano(), sanitizeObjectName(messageID))
	_, err = m.client.PutObject(
		ctx,
		m.bucket,
		objectName,
		bytes.NewReader(body),
		int64(len(body)),
		minio.PutObjectOptions{ContentType: "application/json"},
	)

	return err
}

func (m *MinIO) ensureBucket(ctx context.Context) error {
	exists, err := m.client.BucketExists(ctx, m.bucket)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return m.client.MakeBucket(ctx, m.bucket, minio.MakeBucketOptions{})
}

func sanitizeObjectName(raw string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		" ", "_",
	)
	return replacer.Replace(raw)
}
