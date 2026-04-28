package kafka

import "context"

type Publisher interface {
	Publish(ctx context.Context, key string, topic string, payload []byte) error
}
