# Sentinel Makefile

BINARY_DIR := bin
GOOS       ?= $(shell go env GOOS)
GOARCH     ?= $(shell go env GOARCH)

GO_PKGS    := ./...
MIGRATIONS := internal/store/migrations

.PHONY: all build api agent chain-app ctl tidy fmt vet lint test test-unit \
        compose-up compose-down secrets-init migrate doctor sdk-ts sdk-py ci help

all: build

help:
	@echo "Sentinel make targets:"
	@echo "  build         build all four binaries into ./bin"
	@echo "  test          run unit tests with race detector"
	@echo "  vet           run go vet"
	@echo "  fmt           run gofmt -s -w"
	@echo "  tidy          run go mod tidy"
	@echo "  compose-up    bring up the local dev stack"
	@echo "  compose-down  tear it down (and remove volumes)"
	@echo "  secrets-init  write a placeholder Postgres DSN for compose"
	@echo "  migrate       apply every SQL migration to \$$SENTINEL_POSTGRES_DSN"
	@echo "  doctor        run sentinelctl doctor against \$$SENTINEL_API_ENDPOINT"
	@echo "  sdk-ts        type-check the TypeScript SDK"
	@echo "  sdk-py        sanity-check the Python SDK files"
	@echo "  ci            run every CI gate locally (gofmt, vet, build, test, sdks, openapi)"

## ─── Build ───────────────────────────────────────────────────────────────────

build: api agent chain-app ctl

api:
	@echo "Building sentinel-api..."
	@go build -o $(BINARY_DIR)/sentinel-api ./cmd/sentinel-api

agent:
	@echo "Building sentinel-agent..."
	@go build -o $(BINARY_DIR)/sentinel-agent ./cmd/sentinel-agent

chain-app:
	@echo "Building sentinel-chain-app..."
	@go build -o $(BINARY_DIR)/sentinel-chain-app ./cmd/sentinel-chain-app

ctl:
	@echo "Building sentinelctl..."
	@go build -o $(BINARY_DIR)/sentinelctl ./cmd/sentinelctl

## ─── Deps ────────────────────────────────────────────────────────────────────

tidy:
	go mod tidy

## ─── Quality ─────────────────────────────────────────────────────────────────

fmt:
	gofmt -s -w $(shell git ls-files '*.go' 2>/dev/null || find . -name '*.go' -not -path './vendor/*')

vet:
	go vet $(GO_PKGS)

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not installed; install from https://golangci-lint.run"; exit 1; }
	golangci-lint run $(GO_PKGS)

test: test-unit

test-unit:
	go test -race -count=1 $(GO_PKGS)

## ─── Dev stack ───────────────────────────────────────────────────────────────

# Create the secrets directory and a placeholder DSN for local dev.
secrets-init:
	@mkdir -p deploy/compose/secrets
	@echo "postgresql://sentinel:sentinel_dev@postgres:5432/sentinel?sslmode=disable" \
	  > deploy/compose/secrets/postgres_dsn.txt
	@echo "✓ Secrets placeholder written to deploy/compose/secrets/postgres_dsn.txt"

compose-up: secrets-init
	docker compose -f deploy/compose/docker-compose.yml up --build -d

compose-down:
	docker compose -f deploy/compose/docker-compose.yml down -v

## ─── Migrations ──────────────────────────────────────────────────────────────

migrate:
	@if [ -z "$$SENTINEL_POSTGRES_DSN" ]; then \
	  echo "SENTINEL_POSTGRES_DSN must be set"; exit 1; fi
	@for f in $(MIGRATIONS)/*.sql; do \
	  echo ">> applying $$f"; \
	  psql "$$SENTINEL_POSTGRES_DSN" -v ON_ERROR_STOP=1 -f $$f || exit 1; \
	done
	@echo "✓ migrations applied"

## ─── CLI doctor ──────────────────────────────────────────────────────────────

doctor: ctl
	$(BINARY_DIR)/sentinelctl doctor

## ─── SDKs ────────────────────────────────────────────────────────────────────

sdk-ts:
	@cd sdk/ts && (test -d node_modules || npm install --no-fund --no-audit --silent) && \
	  npx tsc --noEmit -p tsconfig.json

sdk-py:
	python -c "import ast,pathlib; [ast.parse(p.read_text(encoding='utf-8')) for p in pathlib.Path('sdk/python/src').rglob('*.py')]; print('python sdk: parse ok')"

## ─── Local CI ────────────────────────────────────────────────────────────────

ci:
	@bash scripts/ci.sh
