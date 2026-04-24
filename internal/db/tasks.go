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

// sharedSchema mirrors github.com/CHE1-Bot/Bot/schema.sql. Bot, Worker, and
// Dashboard all share one Postgres instance; whichever service boots first
// creates the tables. Every statement is idempotent.
const sharedSchema = `
CREATE TABLE IF NOT EXISTS tickets (
	id             BIGSERIAL PRIMARY KEY,
	guild_id       TEXT NOT NULL,
	channel_id     TEXT NOT NULL,
	user_id        TEXT NOT NULL,
	subject        TEXT NOT NULL,
	status         TEXT NOT NULL,
	transcript_url TEXT,
	opened_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	closed_at      TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS user_levels (
	guild_id   TEXT NOT NULL,
	user_id    TEXT NOT NULL,
	xp         BIGINT NOT NULL DEFAULT 0,
	level      BIGINT NOT NULL DEFAULT 0,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	PRIMARY KEY (guild_id, user_id)
);

CREATE TABLE IF NOT EXISTS mod_logs (
	id           BIGSERIAL PRIMARY KEY,
	guild_id     TEXT NOT NULL,
	moderator_id TEXT NOT NULL,
	target_id    TEXT NOT NULL,
	action       TEXT NOT NULL,
	reason       TEXT,
	created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS applications (
	id         BIGSERIAL PRIMARY KEY,
	guild_id   TEXT NOT NULL,
	user_id    TEXT NOT NULL,
	role       TEXT NOT NULL,
	answers    JSONB NOT NULL,
	status     TEXT NOT NULL DEFAULT 'pending',
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS application_forms (
	guild_id TEXT NOT NULL,
	role     TEXT NOT NULL,
	url      TEXT NOT NULL,
	PRIMARY KEY (guild_id, role)
);

CREATE TABLE IF NOT EXISTS giveaways (
	id         BIGSERIAL PRIMARY KEY,
	guild_id   TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	prize      TEXT NOT NULL,
	winners    TEXT[],
	ends_at    TIMESTAMPTZ NOT NULL,
	status     TEXT NOT NULL
);
`

func (r *TaskRepo) Migrate(ctx context.Context) error {
	if _, err := r.pool.Exec(ctx, tasksSchema); err != nil {
		return err
	}
	if _, err := r.pool.Exec(ctx, sharedSchema); err != nil {
		return err
	}
	return nil
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
