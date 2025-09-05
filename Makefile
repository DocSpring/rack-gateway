.PHONY: go dev dev-build dev-down dev-logs gateway cli mock test test-go test-unit test-integration lint docker clean build all deps tools web-deps web-build web-test web-lint e2e-devrack web-e2e

all: web-build gateway cli mock

build: web-build gateway cli mock

go: gateway cli mock

deps:
	@echo "Installing Go dependencies..."
	@go mod download
	@go mod tidy

tools:
	@echo "Installing development tools..."
	@go install honnef.co/go/tools/cmd/staticcheck@latest
	@go install gotest.tools/gotestsum@latest
	@staticcheck -version
	@gotestsum --version

web-deps:
	@echo "Installing web dependencies..."
	@cd web && pnpm install --frozen-lockfile

web-build: web-deps
	@echo "Building web assets..."
	@cd web && pnpm build

web-test: web-deps
	@echo "Running web tests..."
	@cd web && pnpm test --run

web-lint: web-deps
	@echo "Running web linting..."
	@cd web && pnpm lint

dev:
    @echo "Starting development environment with Docker Compose (rebuild + recreate)..."
    @docker compose up --build --force-recreate

dev-build:
	@echo "Building Docker images for development..."
	@docker compose build

dev-down:
	@echo "Stopping development environment..."
	@docker compose down

dev-logs:
	@echo "Showing development logs..."
	@docker compose logs -f

gateway:
	@echo "Building gateway API server..."
	@go build -o bin/convox-gateway-api ./cmd/gateway/

cli:
	@echo "Building gateway CLI..."
	@go build -ldflags "-X main.Version=1.0.0 -X main.BuildTime=$$(date -u '+%Y-%m-%d_%H:%M:%S')" -o bin/convox-gateway ./cmd/cli/

mock:
	@echo "Building mock Convox server..."
	@go build -o bin/mock-convox ./cmd/mock-convox/

test: test-unit test-integration web-test

test-go: test-unit test-integration

test-unit:
	@echo "Running unit tests..."
	@./scripts/safe-test.sh -v -race -short -timeout 30s ./...

test-integration:
	@echo "Running integration tests..."
	@./scripts/safe-test.sh -v -race -tags=integration -timeout 30s ./internal/integration/...

lint: web-lint
	@echo "Running Go linters..."
	@go vet ./...
	@go fmt ./...
	@staticcheck ./...

docker:
	@echo "Building Docker image..."
	@docker build -t convox-gateway-api:latest .

clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@rm -rf web/dist/
	@rm -rf web/node_modules/
	@go clean -cache

e2e-devrack:
    @echo "Running Convox Development Rack E2E (opt-in via E2E_DEV_RACK=1)..."
    @bash scripts/e2e-devrack.sh

web-e2e:
    @echo "Starting backend services..."
    @docker compose up -d mock-oauth mock-convox gateway-api
    @echo "Running Playwright E2E tests against web dev server..."
    @cd web && pnpm e2e
    @echo "(Backend left running. Use 'make dev-down' to stop.)"
