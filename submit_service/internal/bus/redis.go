package bus

import (
	"context"
	"submit_service/internal/domain"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Config struct {
	RedisAddr string `mapstructure:"redis_addr"`
}

type Producer struct {
	redisClient *redis.Client
}

func NewProducer(redisClient *redis.Client) *Producer {
	return &Producer{redisClient: redisClient}
}

func (p *Producer) ProduceTask(ctx context.Context, task *domain.Task) error {
	payload := ""
	if task.Payload != nil {
		payload = *task.Payload
	}

	if err := p.redisClient.XAdd(ctx, &redis.XAddArgs{
		Stream: "tasks",
		Values: map[string]interface{}{
			"id":      task.ID.String(),
			"status":  string(task.Status),
			"payload": payload,
		},
	}).Err(); err != nil {
		return err
	}
	return nil
}

type Consumer struct {
	redisClient *redis.Client
}

func NewConsumer(redisClient *redis.Client) *Consumer {
	return &Consumer{redisClient: redisClient}
}

func (c *Consumer) ConsumeTasks(ctx context.Context, workerID int, handler func(ctx context.Context, workerID int, task *domain.Task) error) error {
	// Implementation for consuming tasks from Redis stream and processing them with the provided handler
	streams, err := c.redisClient.XRead(ctx, &redis.XReadArgs{
		Streams: []string{"tasks", "0"},
		Block:   0,
	}).Result()
	if err != nil {
		return err
	}

	for _, stream := range streams {
		for _, message := range stream.Messages {
			messageID, err := uuid.Parse(message.ID)
			if err != nil {
				return err
			}

			task := &domain.Task{
				ID:      messageID,
				Status:  domain.StatusProcessing,
				Payload: message.Values["payload"].(*string),
			}
			if err := handler(ctx, workerID, task); err != nil {
				return err
			}
			// Acknowledge the message after processing
			if err := c.redisClient.XAck(ctx, "tasks", "task_group", message.ID).Err(); err != nil {
				return err
			}
		}
	}
	return nil
}

type Service struct {
	Producer *Producer
	Consumer *Consumer
}

func NewService(producer *Producer, consumer *Consumer) *Service {
	return &Service{
		Producer: producer,
		Consumer: consumer,
	}
}