# All targets load the shared .env from the main worktree root (scripts/env-file).
SHELL := /bin/bash
ENV_FILE := $(shell scripts/env-file)
LOAD_ENV := set -a; [ -f "$(ENV_FILE)" ] && . "$(ENV_FILE)"; set +a;
TEST_DATABASE_URL ?= postgres://mailroom:mailroom@127.0.0.1:54329/mailroom?sslmode=disable

.PHONY: help db db-down dev server web-dev web test test-go test-web check build docker env-path

help: ## List targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

env-path: ## Show which .env file is loaded
	@echo $(ENV_FILE)

db: ## Start the shared dev Postgres (docker compose)
	docker compose up -d --wait postgres

db-down: ## Stop the dev Postgres (data kept in a volume)
	docker compose stop postgres

server: ## Run the Go server (API + built PWA) on $$PORT
	@$(LOAD_ENV) go run ./cmd/server

web-dev: ## Run the Vite dev server (proxies /api and /auth to the Go server)
	cd web && npm run dev

dev: db ## Run Go server and Vite dev server together
	@trap 'kill 0' EXIT; $(MAKE) server & $(MAKE) web-dev & wait

web: ## Build the PWA into web/build
	cd web && npm ci && npm run build

test: test-go test-web ## Run all tests

test-go: ## Go tests (needs `make db`)
	@$(LOAD_ENV) TEST_DATABASE_URL="$${TEST_DATABASE_URL:-$(TEST_DATABASE_URL)}" go test ./...

test-web: ## Frontend unit tests
	cd web && npm test

check: ## Static checks: gofmt, go vet, svelte-check
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal; exit 1)
	go vet ./...
	cd web && npm run check

build: web ## Build the server binary into bin/
	CGO_ENABLED=0 go build -o bin/server ./cmd/server

docker: ## Build the production image
	docker build -t zach-mail-room .
