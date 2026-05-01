package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/che1/worker/internal/db"
	"github.com/che1/worker/internal/service"
	"github.com/che1/worker/pkg/models"
)

// DBPinger is the minimal surface needed by /readyz.
type DBPinger interface {
	Ping(ctx context.Context) error
}

// Meta describes the runtime knobs the Dashboard SPA introspects via /api/meta.
// Mirrors the Dashboard's GET /api/meta response shape.
type Meta struct {
	AppEnv              string
	Version             string
	RedisEnabled        bool
	DashboardConfigured bool
}

type Server struct {
	tasks          *service.Tasks
	db             DBPinger
	meta           Meta
	apiKey         string
	allowedOrigins map[string]struct{}
	log            *slog.Logger
}

func NewServer(tasks *service.Tasks, dbPinger DBPinger, meta Meta, apiKey string, allowedOrigins []string, log *slog.Logger) *Server {
	set := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		set[o] = struct{}{}
	}
	return &Server{
		tasks:          tasks,
		db:             dbPinger,
		meta:           meta,
		apiKey:         apiKey,
		allowedOrigins: set,
		log:            log.With("component", "http"),
	}
}

func (s *Server) Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("GET /api/meta", s.metaHandler)
	mux.Handle("POST /api/v1/tasks", s.auth(http.HandlerFunc(s.createTask)))
	mux.Handle("GET /api/v1/tasks", s.auth(http.HandlerFunc(s.listTasks)))
	mux.Handle("GET /api/v1/tasks/{id}", s.auth(http.HandlerFunc(s.getTask)))
	mux.Handle("POST /api/v1/tasks/{id}/complete", s.auth(http.HandlerFunc(s.completeTask)))

	srv := &http.Server{
		Addr:              addr,
		Handler:           s.corsMiddleware(s.logMiddleware(mux)),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	s.log.Info("http server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.apiKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("Authorization")
		if got != "Bearer "+s.apiKey {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if _, ok := s.allowedOrigins[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Max-Age", "600")
			}
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.log.Info("http", "method", r.Method, "path", r.URL.Path, "dur_ms", time.Since(start).Milliseconds())
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.Ping(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "db unavailable")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// metaHandler mirrors the Dashboard's GET /api/meta. The SPA hits this to
// render a status badge / introspect deployment state.
func (s *Server) metaHandler(w http.ResponseWriter, r *http.Request) {
	dbEnabled := false
	if s.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 1*time.Second)
		defer cancel()
		dbEnabled = s.db.Ping(ctx) == nil
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"service":              "worker",
		"app_env":              s.meta.AppEnv,
		"version":              s.meta.Version,
		"db_enabled":           dbEnabled,
		"redis_enabled":        s.meta.RedisEnabled,
		"dashboard_configured": s.meta.DashboardConfigured,
		"server_time":          time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var raw struct {
		Kind      string         `json:"kind"`
		Input     map[string]any `json:"input"`
		CreatedBy string         `json:"created_by"`

		// Dashboard BFF shape: POST /api/v1/tasks with {event, guild_id, payload}.
		Event   string         `json:"event"`
		GuildID string         `json:"guild_id"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	req := models.CreateTaskRequest{
		Kind:      raw.Kind,
		Input:     raw.Input,
		CreatedBy: raw.CreatedBy,
	}
	if raw.Event != "" {
		req.Kind = raw.Event
		req.Input = map[string]any{"guild_id": raw.GuildID, "payload": raw.Payload}
		if req.CreatedBy == "" {
			req.CreatedBy = "dashboard"
		}
	}

	t, err := s.tasks.Create(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := s.tasks.Get(r.Context(), id)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.tasks.List(r.Context(), status, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) completeTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Output map[string]any `json:"output"`
		Error  string         `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if err := s.tasks.Complete(r.Context(), id, body.Output, body.Error); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
