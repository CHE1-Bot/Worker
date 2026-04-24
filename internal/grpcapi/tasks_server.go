//go:build grpcgen

// This file implements the worker.v1.Tasks gRPC service. It depends on the
// stubs produced by `make proto` and is only compiled when the `grpcgen`
// build tag is set (Dockerfile and CI build with this tag). Without the tag
// the gRPC server still exposes Health + reflection; only the Tasks service
// is missing, so local `go build` continues to work before stubs exist.

package grpcapi

import (
	"context"
	"errors"

	workerpb "github.com/che1/worker/gen/workerpb"
	"github.com/che1/worker/internal/db"
	"github.com/che1/worker/internal/service"
	"github.com/che1/worker/pkg/models"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type tasksServer struct {
	workerpb.UnimplementedTasksServer
	svc *service.Tasks
}

func registerTasks(srv *grpc.Server, svc *service.Tasks) {
	workerpb.RegisterTasksServer(srv, &tasksServer{svc: svc})
}

func (s *tasksServer) Create(ctx context.Context, req *workerpb.CreateTaskRequest) (*workerpb.Task, error) {
	if req.GetKind() == "" {
		return nil, status.Error(codes.InvalidArgument, "kind is required")
	}
	t, err := s.svc.Create(ctx, models.CreateTaskRequest{
		Kind:      req.GetKind(),
		Input:     structToMap(req.GetInput()),
		CreatedBy: req.GetCreatedBy(),
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return taskToPB(t), nil
}

func (s *tasksServer) Get(ctx context.Context, req *workerpb.GetTaskRequest) (*workerpb.Task, error) {
	t, err := s.svc.Get(ctx, req.GetId())
	if errors.Is(err, db.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "not found")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return taskToPB(t), nil
}

func (s *tasksServer) List(ctx context.Context, req *workerpb.ListTasksRequest) (*workerpb.ListTasksResponse, error) {
	items, err := s.svc.List(ctx, req.GetStatus(), int(req.GetLimit()))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	out := make([]*workerpb.Task, 0, len(items))
	for i := range items {
		out = append(out, taskToPB(&items[i]))
	}
	return &workerpb.ListTasksResponse{Items: out}, nil
}

func (s *tasksServer) Complete(ctx context.Context, req *workerpb.CompleteTaskRequest) (*workerpb.Task, error) {
	if err := s.svc.Complete(ctx, req.GetId(), structToMap(req.GetResult()), req.GetError()); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	t, err := s.svc.Get(ctx, req.GetId())
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return taskToPB(t), nil
}

func taskToPB(t *models.Task) *workerpb.Task {
	return &workerpb.Task{
		Id:        t.ID,
		Kind:      t.Kind,
		Status:    string(t.Status),
		Input:     mapToStruct(t.Input),
		Result:    mapToStruct(t.Result),
		Error:     t.Error,
		CreatedBy: t.CreatedBy,
		CreatedAt: timestamppb.New(t.CreatedAt),
		UpdatedAt: timestamppb.New(t.UpdatedAt),
	}
}

func mapToStruct(m map[string]any) *structpb.Struct {
	if m == nil {
		return nil
	}
	s, err := structpb.NewStruct(m)
	if err != nil {
		return nil
	}
	return s
}

func structToMap(s *structpb.Struct) map[string]any {
	if s == nil {
		return nil
	}
	return s.AsMap()
}
