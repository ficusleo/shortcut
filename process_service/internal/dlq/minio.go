package dlq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

type Config struct {
	Endpoint  string `mapstructure:"endpoint"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
	DLQBucket string `mapstructure:"dlq_bucket"`
	UseSSL    bool   `mapstructure:"use_ssl"`
}

func NewMinIO(conf *Config) (*MinIO, error) {
	effective := Config{
		Endpoint:  "localhost:9000",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		DLQBucket: "tasks-dlq",
		UseSSL:    false,
	}

	if conf != nil {
		if conf.Endpoint != "" {
			effective.Endpoint = conf.Endpoint
		}
		if conf.AccessKey != "" {
			effective.AccessKey = conf.AccessKey
		}
		if conf.SecretKey != "" {
			effective.SecretKey = conf.SecretKey
		}
		if conf.DLQBucket != "" {
			effective.DLQBucket = conf.DLQBucket
		}
		effective.UseSSL = conf.UseSSL
	}

	client, err := minio.New(effective.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(effective.AccessKey, effective.SecretKey, ""),
		Secure: effective.UseSSL,
	})
	if err != nil {
		return nil, err
	}

	return &MinIO{client: client, bucket: effective.DLQBucket}, nil
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
