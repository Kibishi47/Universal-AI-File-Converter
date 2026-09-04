.PHONY: help dev dev-build down logs logs-api logs-worker logs-web ps clean build test test-api test-web init-bucket

# Default target
help:
	@echo "Commands available for Universal AI File Converter:"
	@echo "  make dev          - Start all local services via Docker Compose"
	@echo "  make dev-build    - Build images and start all local services"
	@echo "  make down         - Stop and remove all containers"
	@echo "  make logs         - Stream logs from all running containers"
	@echo "  make logs-api     - Stream API container logs"
	@echo "  make logs-worker  - Stream Worker container logs"
	@echo "  make logs-web     - Stream Frontend Nuxt 3 container logs"
	@echo "  make ps           - List all running services"
	@echo "  make test         - Run Go tests and Nuxt build checks"
	@echo "  make build        - Compile Go binaries and build frontend bundle locally"
	@echo "  make clean        - Stop containers, remove volumes and temporary build files"
	@echo "  make init-bucket  - Run SeaweedFS bucket initialization script"

# Start dev stack
dev:
	docker compose -f docker/dev/docker-compose.yml up

# Rebuild and start dev stack
dev-build:
	docker compose -f docker/dev/docker-compose.yml up --build

# Stop dev stack
down:
	docker compose -f docker/dev/docker-compose.yml down

# Logs
logs:
	docker compose -f docker/dev/docker-compose.yml logs -f

logs-api:
	docker compose -f docker/dev/docker-compose.yml logs -f api

logs-worker:
	docker compose -f docker/dev/docker-compose.yml logs -f worker

logs-web:
	docker compose -f docker/dev/docker-compose.yml logs -f web

# Status
ps:
	docker compose -f docker/dev/docker-compose.yml ps

# Local tests
test: test-api test-web

test-api:
	cd apps/api && go test ./...

test-web:
	cd apps/web && npm run build

# Local build
build:
	cd apps/api && go build -v ./cmd/server && go build -v ./cmd/worker
	cd apps/web && npm run build

# Bucket initialization helper
init-bucket:
	./scripts/init-seaweedfs.sh

# Clean
clean:
	docker compose -f docker/dev/docker-compose.yml down -v --remove-orphans
	rm -f apps/api/server apps/api/worker
	rm -rf apps/web/.output apps/web/.nuxt apps/web/dist
