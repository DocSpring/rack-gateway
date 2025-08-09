.PHONY: dev proxy cli test lint docker clean build all

all: proxy cli

build: proxy cli

dev:
	@echo "Starting gateway API in development mode..."
	@./scripts/dev_env.sh && go run cmd/api/main.go

proxy:
	@echo "Building gateway API server..."
	@go build -o bin/convox-gateway-api cmd/api/main.go

cli:
	@echo "Building gateway CLI..."
	@go build -o bin/convox-gateway cmd/cli/main.go

test:
	@echo "Running tests..."
	@go test -v -race ./...

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