.PHONY: dev proxy cli mock test test-unit test-integration lint docker clean build all deps

all: proxy cli mock

build: proxy cli mock

deps:
	@echo "Installing Go dependencies..."
	@go mod download
	@go mod tidy

dev:
	@echo "Starting gateway API in development mode..."
	@./scripts/dev_env.sh && go run cmd/api/main.go

proxy:
	@echo "Building gateway API server..."
	@go build -o bin/convox-gateway-api cmd/api/main.go

cli:
	@echo "Building gateway CLI..."
	@go build -o bin/convox-gateway cmd/cli/main.go

mock:
	@echo "Building mock Convox server..."
	@go build -o bin/mock-convox cmd/mock-convox/main.go

test: test-unit test-integration

test-unit:
	@echo "Running unit tests..."
	@./scripts/safe-test.sh -v -race -short ./...

test-integration:
	@echo "Running integration tests..."
	@./scripts/safe-test.sh -v -race -tags=integration ./internal/integration/...

lint:
	@echo "Running linters..."
	@go vet ./...
	@go fmt ./...
	@if command -v staticcheck > /dev/null; then staticcheck ./...; else echo "staticcheck not installed"; fi

docker:
	@echo "Building Docker image..."
	@docker build -t convox-gateway-api:latest .

clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@go clean -cache
