.DEFAULT_GOAL := help
SHELL := /bin/bash

# Go package list, excluding vendored JS toolchain code that `bun install`
# drops under ui/node_modules (e.g. flatted/golang).
GO_PKGS = $(shell go list ./... | grep -v '/node_modules/')

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
ui-test: ## Run UI tests
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
	go test $(GO_PKGS)

.PHONY: race
race: ## Run the full Go test suite under the race detector
	go test -race $(GO_PKGS)

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: vuln
vuln: ## Scan for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# --- Gate -----------------------------------------------------------------

.PHONY: gate
gate: ui-build build vet test race ui-typecheck ui-lint ## The full verification gate (AGENTS.md §4)
	@echo "✅ gate green"

.PHONY: graph
graph: ## Refresh the graphify knowledge graph
	rtk graphify update .
