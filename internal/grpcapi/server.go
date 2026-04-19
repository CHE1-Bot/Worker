package grpcapi

import (
	"context"
	"log/slog"
	"net"

	"github.com/che1/worker/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

type Server struct {
	tasks  *service.Tasks
	apiKey string
	log    *slog.Logger
	srv    *grpc.Server
}

func NewServer(tasks *service.Tasks, apiKey string, log *slog.Logger) *Server {
	return &Server{tasks: tasks, apiKey: apiKey, log: log.With("component", "grpc")}
}

func (s *Server) Serve(ctx context.Context, addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	s.srv = grpc.NewServer(
		grpc.UnaryInterceptor(s.authUnary),
	)

	// Health + reflection are always available.
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(s.srv, hs)
	reflection.Register(s.srv)

	// Register your generated service here once you run `make proto`:
	//   workerpb.RegisterTasksServer(s.srv, newTasksServer(s.tasks))

	go func() {
		<-ctx.Done()
		s.srv.GracefulStop()
	}()

	s.log.Info("grpc server listening", "addr", addr)
	if err := s.srv.Serve(lis); err != nil {
		return err
	}
	return nil
}

func (s *Server) authUnary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if s.apiKey == "" {
		return handler(ctx, req)
	}
	// Skip auth for health checks.
	if info.FullMethod == "/grpc.health.v1.Health/Check" || info.FullMethod == "/grpc.health.v1.Health/Watch" {
		return handler(ctx, req)
	}
	md, _ := metadata.FromIncomingContext(ctx)
	tokens := md.Get("authorization")
	if len(tokens) == 0 || tokens[0] != "Bearer "+s.apiKey {
		return nil, status.Error(codes.Unauthenticated, "unauthorized")
	}
	return handler(ctx, req)
}
