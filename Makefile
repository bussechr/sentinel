# Sentinel Makefile

BINARY_DIR := bin
GOOS       ?= $(shell go env GOOS)
GOARCH     ?= $(shell go env GOARCH)

.PHONY: all build api agent chain-app ctl tidy lint test test-unit compose-up compose-down migrate doctor

all: build

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

lint:
	golangci-lint run ./...

test: test-unit

test-unit:
	go test -race -count=1 ./...

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
	psql "$$SENTINEL_POSTGRES_DSN" -f internal/store/migrations/001_initial.sql

## ─── CLI doctor ──────────────────────────────────────────────────────────────

doctor: ctl
	$(BINARY_DIR)/sentinelctl doctor
