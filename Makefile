.PHONY: run build build-grpc tidy test proto proto-tools

run:
	go run ./cmd/worker

build:
	go build -o bin/worker ./cmd/worker

# Build with the Tasks gRPC service registered. Requires `make proto` first.
build-grpc:
	go build -tags grpcgen -o bin/worker ./cmd/worker

tidy:
	go mod tidy

test:
	go test ./...

# Install the Go protoc plugins into $GOBIN (protoc itself must be on PATH).
proto-tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate Go stubs into gen/workerpb/ (matches the proto's go_package option).
# Requires protoc + protoc-gen-go + protoc-gen-go-grpc on PATH.
proto:
	mkdir -p gen/workerpb
	protoc -I proto \
	       --go_out=gen/workerpb --go_opt=paths=source_relative \
	       --go-grpc_out=gen/workerpb --go-grpc_opt=paths=source_relative \
	       worker.proto
