package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"process_service/internal/dlq"
	"process_service/internal/domain"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Config struct {
	RedisAddr string `mapstructure:"redis_addr"`
}

type Producer struct {
	Client *redis.Client
}


type ExternalAPICaller interface {
	GetSomething(ctx context.Context, taskID string, workerID int) error
}

func NewProducer(redisClient *redis.Client) *Producer {
	return &Producer{Client: redisClient}
}

func (p *Producer) ProduceTask(ctx context.Context, task *domain.Task) error {
	if err := p.Client.XAdd(ctx, &redis.XAddArgs{
		Stream: "tasks",
		Values: task,
	}).Err(); err != nil {
		return err
	}
	return nil
}

type Consumer struct {
	Client      *redis.Client
	dlqWriter   dlq.Writer
	statusHook  InvalidTaskStatusUpdater
}

type InvalidTaskStatusUpdater interface {
	MarkTaskInvalid(ctx context.Context, taskID uuid.UUID, failedPayload *string, reason string) error
}

func NewConsumer(redisClient *redis.Client, dlqWriter dlq.Writer, statusHook ...InvalidTaskStatusUpdater) *Consumer {
	c := &Consumer{Client: redisClient, dlqWriter: dlqWriter}

	if len(statusHook) > 0 {
		c.statusHook = statusHook[0]
	}

	return c
}

func (c *Consumer) ConsumeTasks(ctx context.Context, apiCaller ExternalAPICaller, workerID int, 
	handler func(ctx context.Context, apiCaller ExternalAPICaller, workerID int, task *domain.Task) error) error {
	// Implementation for consuming tasks from Redis stream and processing them with the provided handler
	streams, err := c.Client.XRead(ctx, &redis.XReadArgs{
		Streams: []string{"tasks", "0"},
		Block:   0,
	}).Result()
	if err != nil {
		return err
	}

	for _, stream := range streams {
		for _, message := range stream.Messages {
			taskID, hasTaskID := extractTaskUUID(message)

			rawPayload, ok := message.Values["payload"]
			if !ok {
				if err := c.handleInvalidPayload(ctx, message, hasTaskID, taskID, nil, "missing payload field"); err != nil {
					return err
				}
				if err := c.ackMessage(ctx, message.ID); err != nil {
					return err
				}
				continue
			}

			var payloadBytes []byte
			switch v := rawPayload.(type) {
			case string:
				payloadBytes = []byte(v)
			case []byte:
				payloadBytes = v
			default:
				if err := c.handleInvalidPayload(ctx, message, hasTaskID, taskID, []byte(fmt.Sprintf("%v", v)), "unsupported payload type"); err != nil {
					return err
				}
				if err := c.ackMessage(ctx, message.ID); err != nil {
					return err
				}
				continue
			}

			task := &domain.Task{}
			if err := json.Unmarshal(payloadBytes, task); err != nil {
				if dlqErr := c.handleInvalidPayload(ctx, message, hasTaskID, taskID, payloadBytes, err.Error()); dlqErr != nil {
					return dlqErr
				}
				if err := c.ackMessage(ctx, message.ID); err != nil {
					return err
				}
				continue
			}

			if hasTaskID {
				task.ID = taskID
			}
			task.Status = domain.StatusProcessing
			if err := handler(ctx, apiCaller, workerID, task); err != nil {
				return err
			}
			if err := c.ackMessage(ctx, message.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Consumer) ackMessage(ctx context.Context, messageID string) error {
	return c.Client.XAck(ctx, "tasks", "task_group", messageID).Err()
}

func (c *Consumer) handleInvalidPayload(ctx context.Context, message redis.XMessage, hasTaskID bool, taskID uuid.UUID, payload []byte, reason string) error {
	if hasTaskID && c.statusHook != nil {
		var failedPayload *string
		if payload != nil {
			v := string(payload)
			failedPayload = &v
		}
		if err := c.statusHook.MarkTaskInvalid(ctx, taskID, failedPayload, reason); err != nil {
			return err
		}
	}

	return c.sendToDLQ(ctx, message.ID, payload, reason)
}

func (c *Consumer) sendToDLQ(ctx context.Context, messageID string, payload []byte, reason string) error {
	if c.dlqWriter == nil {
		return fmt.Errorf("dlq storage is not configured")
	}

	return c.dlqWriter.SendInvalidPayload(ctx, messageID, payload, reason)
}

func extractTaskUUID(message redis.XMessage) (uuid.UUID, bool) {
	candidates := []string{"id", "ID", "task_id", "taskId"}
	for _, key := range candidates {
		raw, ok := message.Values[key]
		if !ok {
			continue
		}

		switch v := raw.(type) {
		case string:
			parsed, err := uuid.Parse(v)
			if err == nil {
				return parsed, true
			}
		case fmt.Stringer:
			parsed, err := uuid.Parse(v.String())
			if err == nil {
				return parsed, true
			}
		}
	}

	return uuid.Nil, false
}



