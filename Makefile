BINARY_NAME=watcher
DOCKER_COMPOSE=docker-compose

.PHONY: all build run test clean docker-up docker-down migrate-up migrate-down lint

all: build

build:
	@echo "Building $(BINARY_NAME)..."
	@go build -o bin/$(BINARY_NAME) ./cmd/watcher

install: build
	@echo "Installing $(BINARY_NAME) to $(GOPATH)/bin..."
	@cp bin/$(BINARY_NAME) $(shell go env GOPATH)/bin/

run: build
	@echo "Running $(BINARY_NAME)..."
	@./bin/$(BINARY_NAME)

test:
	@echo "Running unit and integration tests..."
	@go test -v ./internal/...

test-e2e:
	@echo "Running live E2E tests (requires E2E_LIVE=true)..."
	@E2E_LIVE=true go test -v ./tests/e2e/...

clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@go clean

lint:
	@echo "Running linter..."
	@golangci-lint run

docker-up:
	@echo "Starting docker services..."
	@$(DOCKER_COMPOSE) up -d

docker-down:
	@echo "Stopping docker services..."
	@$(DOCKER_COMPOSE) down

migrate-up:
	@echo "Running migrations..."
	@go run github.com/pressly/goose/v3/cmd/goose -dir migrations postgres "postgres://watcher:watcher123@localhost:5432/watcher?sslmode=disable" up

migrate-down:
	@echo "Rolling back migrations..."
	@go run github.com/pressly/goose/v3/cmd/goose -dir migrations postgres "postgres://watcher:watcher123@localhost:5432/watcher?sslmode=disable" down

help:
	@echo "Available commands:"
	@echo "  make build         - Build the application"
	@echo "  make install       - Build and install to GOPATH/bin"
	@echo "  make run           - Build and run the application"
	@echo "  make test          - Run tests"
	@echo "  make clean         - Clean build artifacts"
	@echo "  make docker-up     - Start Docker dependencies"
	@echo "  make docker-down   - Stop Docker dependencies"
	@echo "  make migrate-up    - Run database migrations"
	@echo "  make migrate-down  - Rollback database migrations"
