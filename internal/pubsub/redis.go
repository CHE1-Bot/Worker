package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/che1/worker/pkg/models"
	"github.com/redis/go-redis/v9"
)

type Publisher struct {
	rdb     *redis.Client
	channel string
	log     *slog.Logger
}

func NewPublisher(addr, password string, db int, channel string, log *slog.Logger) (*Publisher, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password, DB: db})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Publisher{rdb: rdb, channel: channel, log: log.With("component", "pubsub")}, nil
}

func (p *Publisher) Publish(ctx context.Context, evt models.Event) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	if err := p.rdb.Publish(ctx, p.channel, data).Err(); err != nil {
		return fmt.Errorf("redis publish: %w", err)
	}
	p.log.Debug("event published", "id", evt.ID, "type", evt.Type)
	return nil
}

func (p *Publisher) Close() error { return p.rdb.Close() }
