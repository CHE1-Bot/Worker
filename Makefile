.PHONY: run build tidy test proto

run:
	go run ./cmd/worker

build:
	go build -o bin/worker ./cmd/worker

tidy:
	go mod tidy

test:
	go test ./...

# Generate Go stubs from proto/. Requires protoc + protoc-gen-go + protoc-gen-go-grpc.
proto:
	protoc --go_out=. --go_opt=paths=source_relative \
	       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
	       proto/worker.proto
