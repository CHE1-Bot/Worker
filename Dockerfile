# syntax=docker/dockerfile:1.7

# Builder: install protoc + Go plugins, generate stubs, build static binary.
FROM golang:1.22-alpine AS build
RUN apk add --no-cache protoc make git

WORKDIR /src

# Cache Go module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Install Go plugins for protoc. Pinned to versions compatible with go.mod's
# google.golang.org/grpc and google.golang.org/protobuf.
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.1 && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.4.0

COPY . .

# Generate worker.v1 stubs and build with the grpcgen tag so the Tasks
# service is registered alongside Health + reflection.
ARG BUILD_VERSION=docker
RUN make proto && \
    CGO_ENABLED=0 GOOS=linux go build \
        -tags grpcgen \
        -trimpath \
        -ldflags "-s -w -X main.buildVersion=${BUILD_VERSION}" \
        -o /out/worker ./cmd/worker

# Runtime: distroless nonroot, matches the CHE1 Dashboard image family.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/worker /worker
EXPOSE 8080 8090 9090
USER nonroot:nonroot
ENTRYPOINT ["/worker"]
