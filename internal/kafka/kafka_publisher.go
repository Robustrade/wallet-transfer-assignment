package kafka

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

type KafkaPublisher struct {
	writer *kafka.Writer
}

func NewKafkaPublisher() *KafkaPublisher {
	brokers := brokerListFromEnv()
	return &KafkaPublisher{
		writer: kafka.NewWriter(kafka.WriterConfig{
			Brokers:      brokers,
			Balancer:     &kafka.Hash{},
			RequiredAcks: int(kafka.RequireAll),
			Async:        false,
			BatchTimeout: 100 * time.Millisecond,
		}),
	}
}

func brokerListFromEnv() []string {
	brokers := strings.TrimSpace(os.Getenv("KAFKA_BROKERS"))
	if brokers == "" {
		return []string{"localhost:9092"}
	}
	return strings.Split(brokers, ",")
}

func (k *KafkaPublisher) Publish(ctx context.Context, key string, topic string, payload []byte) error {
	if k.writer == nil {
		return fmt.Errorf("kafka writer is not initialized")
	}
	if strings.TrimSpace(topic) == "" {
		return fmt.Errorf("kafka topic is required")
	}

	msg := kafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: payload,
		Time:  time.Now(),
	}

	return k.writer.WriteMessages(ctx, msg)
}
