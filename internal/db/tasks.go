package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/che1/worker/pkg/models"
	"github.com/jackc/pgx/v5"
)

type TaskRepo struct {
	pool *Pool
}

func NewTaskRepo(pool *Pool) *TaskRepo { return &TaskRepo{pool: pool} }

var ErrNotFound = errors.New("not found")

const tasksSchema = `
CREATE TABLE IF NOT EXISTS tasks (
	id           TEXT PRIMARY KEY,
	kind         TEXT NOT NULL,
	status       TEXT NOT NULL,
	input        JSONB,
	result       JSONB,
	error        TEXT,
	created_by   TEXT,
	created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS tasks_status_idx ON tasks(status);
`

func (r *TaskRepo) Migrate(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, tasksSchema)
	return err
}

func (r *TaskRepo) Create(ctx context.Context, t *models.Task) error {
	if t.ID == "" {
		return fmt.Errorf("task id required")
	}
	input, _ := json.Marshal(t.Input)
	now := time.Now().UTC()
	t.CreatedAt, t.UpdatedAt = now, now
	_, err := r.pool.Exec(ctx, `
		INSERT INTO tasks (id, kind, status, input, created_by, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, t.ID, t.Kind, t.Status, input, t.CreatedBy, t.CreatedAt, t.UpdatedAt)
	return err
}

func (r *TaskRepo) Get(ctx context.Context, id string) (*models.Task, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, kind, status, input, result, error, created_by, created_at, updated_at
		FROM tasks WHERE id = $1
	`, id)
	var t models.Task
	var input, result []byte
	err := row.Scan(&t.ID, &t.Kind, &t.Status, &input, &result, &t.Error, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(input) > 0 {
		_ = json.Unmarshal(input, &t.Input)
	}
	if len(result) > 0 {
		_ = json.Unmarshal(result, &t.Result)
	}
	return &t, nil
}

func (r *TaskRepo) UpdateStatus(ctx context.Context, id string, status models.TaskStatus, result map[string]any, errMsg string) error {
	res, _ := json.Marshal(result)
	tag, err := r.pool.Exec(ctx, `
		UPDATE tasks SET status=$2, result=$3, error=$4, updated_at=now() WHERE id=$1
	`, id, status, res, errMsg)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *TaskRepo) List(ctx context.Context, status string, limit int) ([]models.Task, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows pgx.Rows
	var err error
	if status == "" {
		rows, err = r.pool.Query(ctx, `
			SELECT id, kind, status, input, result, error, created_by, created_at, updated_at
			FROM tasks ORDER BY created_at DESC LIMIT $1`, limit)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT id, kind, status, input, result, error, created_by, created_at, updated_at
			FROM tasks WHERE status=$1 ORDER BY created_at DESC LIMIT $2`, status, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.Task, 0, limit)
	for rows.Next() {
		var t models.Task
		var input, result []byte
		if err := rows.Scan(&t.ID, &t.Kind, &t.Status, &input, &result, &t.Error, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		if len(input) > 0 {
			_ = json.Unmarshal(input, &t.Input)
		}
		if len(result) > 0 {
			_ = json.Unmarshal(result, &t.Result)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
