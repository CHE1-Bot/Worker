package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/che1/worker/internal/db"
	"github.com/che1/worker/internal/pubsub"
	"github.com/che1/worker/internal/ws"
	"github.com/che1/worker/pkg/models"
)

// Tasks is the core business-logic layer. REST and gRPC both go through here.
type Tasks struct {
	repo *db.TaskRepo
	pub  *pubsub.Publisher
	hub  *ws.Hub
	log  *slog.Logger
}

func NewTasks(repo *db.TaskRepo, pub *pubsub.Publisher, hub *ws.Hub, log *slog.Logger) *Tasks {
	return &Tasks{repo: repo, pub: pub, hub: hub, log: log.With("component", "tasks")}
}

func (s *Tasks) Create(ctx context.Context, req models.CreateTaskRequest) (*models.Task, error) {
	if req.Kind == "" {
		return nil, fmt.Errorf("kind is required")
	}
	t := &models.Task{
		ID:        newID(),
		Kind:      req.Kind,
		Status:    models.TaskStatusPending,
		Input:     req.Input,
		CreatedBy: req.CreatedBy,
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return nil, fmt.Errorf("persist task: %w", err)
	}
	s.emit(models.Event{
		ID:      t.ID,
		Type:    models.EventTaskCreated,
		Subject: t.ID,
		Payload: map[string]any{
			"id":         t.ID,
			"kind":       t.Kind,
			"status":     t.Status,
			"input":      t.Input,
			"created_by": t.CreatedBy,
		},
		Timestamp: time.Now().UTC(),
	})
	return t, nil
}

func (s *Tasks) Get(ctx context.Context, id string) (*models.Task, error) {
	return s.repo.Get(ctx, id)
}

func (s *Tasks) List(ctx context.Context, status string, limit int) ([]models.Task, error) {
	return s.repo.List(ctx, status, limit)
}

func (s *Tasks) Complete(ctx context.Context, id string, result map[string]any, errMsg string) error {
	status := models.TaskStatusSucceeded
	if errMsg != "" {
		status = models.TaskStatusFailed
	}
	if err := s.repo.UpdateStatus(ctx, id, status, result, errMsg); err != nil {
		return err
	}
	t, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	s.emit(models.Event{
		ID:      t.ID,
		Type:    models.EventTaskCompleted,
		Subject: t.ID,
		Payload: map[string]any{
			"id":         t.ID,
			"kind":       t.Kind,
			"status":     t.Status,
			"input":      t.Input,
			"output":     t.Result,
			"error":      t.Error,
			"created_by": t.CreatedBy,
		},
		Timestamp: time.Now().UTC(),
	})
	return nil
}

func (s *Tasks) emit(evt models.Event) {
	if s.hub != nil {
		s.hub.Broadcast(evt)
	}
	if s.pub != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := s.pub.Publish(ctx, evt); err != nil {
			s.log.Warn("publish event", "err", err)
		}
	}
}

func newID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
