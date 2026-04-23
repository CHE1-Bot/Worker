package models

import "time"

type EventType string

const (
	EventTaskCreated   EventType = "task.created"
	EventTaskUpdated   EventType = "task.updated"
	EventTaskCompleted EventType = "task.completed"
)

type Event struct {
	ID        string         `json:"id"`
	Type      EventType      `json:"type"`
	Source    string         `json:"source,omitempty"`
	Subject   string         `json:"subject,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusSucceeded TaskStatus = "success"
	TaskStatusFailed    TaskStatus = "failed"
)

type Task struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Status    TaskStatus     `json:"status"`
	Input     map[string]any `json:"input,omitempty"`
	Result    map[string]any `json:"output,omitempty"`
	Error     string         `json:"error,omitempty"`
	CreatedBy string         `json:"created_by,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type CreateTaskRequest struct {
	Kind      string         `json:"kind"`
	Input     map[string]any `json:"input,omitempty"`
	CreatedBy string         `json:"created_by,omitempty"`
}
