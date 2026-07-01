GOBIN := $(shell go env GOPATH)/bin
export PATH := $(PATH):$(GOBIN)

.PHONY: tools generate tidy build run test

# Install codegen plugins pinned alongside the module.
tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate Go stubs and gRPC server from proto.
generate:
	buf generate

tidy:
	go mod tidy

build:
	go build -o bin/server ./cmd/server

run:
	go run ./cmd/server

# Integration tests need a Postgres reachable at TEST_DATABASE_URL (defaults to
# the local ledger DB). They skip cleanly if none is reachable.
test:
	go test ./...
