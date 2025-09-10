.PHONY: check go dev dev-build dev-down dev-logs gateway cli mock test go-test go-test-unit go-test-integration lint go-lint docker clean build all deps tools web-deps web-build web-test web-lint e2e-devrack web-e2e e2e-cli e2e-cli-dev e2e-cli-release web-e2e-dev web-e2e-release free-prod-gateway free-dev-gateway

# Run all linters and tests by default
.DEFAULT_GOAL := check

# ----- Helpers (avoid duplication) -----
define UP_DEV_STACK
$(MAKE) free-prod-gateway
docker compose up -d --build mock-oauth mock-convox gateway-api-dev web-dev
WEB_PORT=$${WEB_PORT:-5173} GATEWAY_PORT=$${GATEWAY_PORT:-8447} MOCK_OAUTH_PORT=$${MOCK_OAUTH_PORT:-3345} bash scripts/wait-services.sh
endef

define UP_RELEASE_STACK
$(MAKE) free-dev-gateway
docker compose up -d --build mock-oauth mock-convox gateway-api
WEB_PORT=$${GATEWAY_PORT:-8447} GATEWAY_PORT=$${GATEWAY_PORT:-8447} MOCK_OAUTH_PORT=$${MOCK_OAUTH_PORT:-3345} CHECK_VITE_PROXY=false bash scripts/wait-services.sh
endef

define INSTALL_PLAYWRIGHT
cd web && pnpm install --frozen-lockfile && pnpm exec playwright install --with-deps || pnpm exec playwright install
endef

define RUN_WEB_E2E_DEV
$(INSTALL_PLAYWRIGHT)
cd web && env WEB_PORT=$${WEB_PORT:-5173} pnpm e2e
endef

define RUN_WEB_E2E_RELEASE
$(INSTALL_PLAYWRIGHT)
cd web && env GATEWAY_PORT=$${GATEWAY_PORT:-8447} pnpm e2e
endef

define RUN_CLI_E2E
bash scripts/cli-e2e.sh
endef

define TEARDOWN
docker compose down -v --remove-orphans || true
endef

# Aggregate target: run all linters and all tests
check: lint test

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

db-test-create:
	@echo "Ensuring test database exists (convox_gateway_test)..."
	@psql -Atqc "SELECT 1 FROM pg_database WHERE datname='convox_gateway_test'" >/dev/null 2>&1 \
		|| createdb convox_gateway_test

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
	@echo "Bringing up dev stack..."
	@docker compose --profile dev up --build --force-recreate postgres mock-oauth mock-convox web-dev gateway-api-dev

dev-build:
	@echo "Building Docker images for development..."
	@docker compose build

dev-down:
	@echo "Stopping development environment..."
	@docker compose down -v --remove-orphans || true

.PHONY: dev-nuke
dev-nuke:
	@echo "Force-removing any leftover Compose containers and network for project 'convox-gateway'..."
	@docker ps -a --filter "label=com.docker.compose.project=convox-gateway" -q | xargs -r docker rm -f || true
	@docker network ls --format '{{.Name}}' | grep -E '^convox-gateway_gateway-net$$' >/dev/null 2>&1 && docker network rm convox-gateway_gateway-net || true

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

test: go-test web-test web-e2e e2e-cli

# Standardized Go test targets
go-test: go-test-unit go-test-integration

go-test-unit:
	@echo "Running Go unit tests..."
	@./scripts/safe-test.sh -v -race -short -timeout 30s ./...

go-test-integration:
	@echo "Running Go integration tests..."
	@./scripts/safe-test.sh -v -race -tags=integration -timeout 30s ./internal/integration/...



go-lint:
	@echo "Running Go linters..."
	@go vet ./...
	@go fmt ./...
	@staticcheck ./...

lint: web-lint go-lint
	@echo "All linters passed"

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


e2e-cli:
	@echo "Starting dev stack for CLI E2E..."
	@$(UP_DEV_STACK)
	@echo "Running CLI E2E..."
	@$(RUN_CLI_E2E)
	@echo "(Backend left running. Use 'make dev-down' to stop.)"

e2e-cli-dev:
	@echo "Starting dev stack for CLI E2E..."
	@$(UP_DEV_STACK)
	@echo "Running CLI E2E..."
	@$(RUN_CLI_E2E)
	@echo "(Backend left running. Use 'make dev-down' to stop.)"

e2e-cli-release:
	@echo "Starting release stack for CLI E2E..."
	@$(UP_RELEASE_STACK)
	@echo "Running CLI E2E..."
	@$(RUN_CLI_E2E)
	@echo "Tearing down prod-like backend..."
	@$(TEARDOWN)

web-e2e:
	@echo "[alias] Running web-e2e-release (use web-e2e-dev or web-e2e-release explicitly)"
	@$(MAKE) web-e2e-dev

web-e2e-dev:
	@echo "Starting dev backend for Web E2E..."
	@$(UP_DEV_STACK)
	@echo "Running Web E2E (dev)..."
	@$(RUN_WEB_E2E_DEV)
	@echo "(Backend left running. Use 'make dev-down' to stop.)"

web-e2e-release:
	@echo "Starting prod-like backend for Web E2E..."
	@$(UP_RELEASE_STACK)
	@echo "Running Web E2E (release)..."
	@$(RUN_WEB_E2E_RELEASE)
	@echo "Tearing down prod-like backend..."
	@$(TEARDOWN)

free-prod-gateway:
	@echo "Ensuring prod gateway-api is not occupying port 8447..."
	@docker compose ps -q gateway-api >/dev/null 2>&1 && (docker compose stop gateway-api && docker compose rm -f gateway-api) || true

free-dev-gateway:
	@echo "Ensuring dev gateway-api-dev is not occupying port 8447..."
	@docker compose ps -q gateway-api-dev >/dev/null 2>&1 && (docker compose stop gateway-api-dev && docker compose rm -f gateway-api-dev) || true
