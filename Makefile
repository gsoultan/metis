.DEFAULT_GOAL := help
SHELL := /bin/bash

# Go package list, excluding vendored JS toolchain code that `bun install`
# drops under ui/node_modules (e.g. flatted/golang).
GO_PKGS = $(shell go list ./... | grep -v '/node_modules/')

# Extra flags for the test targets. Empty by default, so a developer re-running
# the gate keeps Go's test cache and the fast second run that comes with it.
# CI passes -count=1: its runner keeps GOCACHE between jobs, and a cached suite
# reports "ok (cached)" without executing anything — which is indistinguishable
# from a suite that ran and passed.
GO_TEST_FLAGS ?=

.PHONY: help
help: ## Show available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# --- Development ----------------------------------------------------------

.PHONY: dev
dev: ## Run backend + UI together with hot reload (Ctrl-C stops both)
	./scripts/dev.sh

.PHONY: dev-backend
dev-backend: ## Run only the Go backend
	./scripts/dev.sh backend

.PHONY: dev-ui
dev-ui: ## Run only the Vite dev server
	./scripts/dev.sh ui

.PHONY: dev-reset
dev-reset: ## Wipe the local database and config, then run everything
	./scripts/dev.sh --reset

# --- UI -------------------------------------------------------------------

.PHONY: ui-deps
ui-deps: ## Install UI dependencies
	cd ui && bun install

.PHONY: ui-build
ui-build: ## Build the UI (required before any Go build — ui/embed.go embeds ui/dist)
	cd ui && bun run build

.PHONY: ui-typecheck
ui-typecheck: ## Typecheck the UI
	# Uses tsgo, the TypeScript 7 native compiler (~16x faster than tsc here).
	# See ui/TYPESCRIPT.md for why typescript@5.9 is also installed.
	cd ui && bun run typecheck

.PHONY: ui-lint
ui-lint: ## Lint the UI
	cd ui && bun run lint

.PHONY: ui-test
ui-test: ## Run the UI tests
	# This target existed and the gate did not call it, so the tests under it
	# had not run in a long time. Logic in the browser is as easy to get wrong
	# as logic on the server.
	cd ui && bun test

# --- Go -------------------------------------------------------------------

.PHONY: build
build: ## Build all Go packages
	go build ./...

.PHONY: vet
vet: ## Run go vet across the whole module
	go vet $(GO_PKGS)

.PHONY: test
test: ## Run the full Go test suite (NOT ./server/... — that skips tests/)
	go test $(GO_TEST_FLAGS) $(GO_PKGS)

.PHONY: test-db
test-db: ## Run the tests that need a real database (Postgres/MySQL); see AGENTS.md §4
	@echo "Postgres and MySQL tests skip unless METIS_TEST_POSTGRES_DSN / METIS_TEST_MYSQL_DSN are set."
	@echo "Apple container:"
	@echo "  container run -d --rm --name metis-pg -e POSTGRES_PASSWORD=metis -e POSTGRES_USER=metis -e POSTGRES_DB=metis docker.io/library/postgres:17"
	@echo "  export METIS_TEST_POSTGRES_DSN=\"host=<ip> user=metis password=metis dbname=metis port=5432 sslmode=disable\""
	go test ./tests/postgres/... ./tests/mysqldb/... -v

.PHONY: race
race: ## Run the full Go test suite under the race detector
	go test -race $(GO_TEST_FLAGS) $(GO_PKGS)

.PHONY: strict-scope
strict-scope: ## Run the strict-tenant-scope suite with the flag on, as production would set it
	METIS_FEATURE_STRICT_TENANT_SCOPE=true go test -count=1 ./tests/strictscope/...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: vuln
vuln: ## Scan for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# --- Gate -----------------------------------------------------------------

.PHONY: sdk
sdk: ## Build and test the Go client SDK (its own module — the main gate skips it)
	cd sdk && go vet ./... && go test -race $(GO_TEST_FLAGS) ./...

.PHONY: gate
gate: ui-build build vet test race strict-scope sdk ui-typecheck ui-lint ui-test ## The full verification gate (AGENTS.md §4)
	@echo "✅ gate green"

.PHONY: docker
docker: ## Build the production image, stamped with the current git describe
	docker build --build-arg VERSION=$$(git describe --tags --always --dirty) -t metis:$$(git describe --tags --always --dirty) -t metis:latest .

.PHONY: docker-run
docker-run: ## Bring up the evaluation stack (engine + PostgreSQL) on :8080
	docker compose up --build

.PHONY: graph
graph: ## Refresh the graphify knowledge graph
	rtk graphify update .
