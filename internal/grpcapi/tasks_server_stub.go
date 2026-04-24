//go:build !grpcgen

package grpcapi

import (
	"github.com/che1/worker/internal/service"
	"google.golang.org/grpc"
)

// registerTasks is a no-op unless the generated proto stubs are compiled in.
// Build with `-tags grpcgen` after running `make proto` to enable the Tasks
// service. The gRPC server still serves Health + reflection in either case.
func registerTasks(_ *grpc.Server, _ *service.Tasks) {}
