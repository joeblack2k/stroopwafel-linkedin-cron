SHELL := /bin/bash

.PHONY: build run run-scheduler test clean fmt lint

build:
	@mkdir -p bin
	go build -o bin/linkedin-cron-server ./cmd/server
	go build -o bin/linkedin-cron-scheduler ./cmd/scheduler

run:
	@set -a; [ -f .env ] && source .env; set +a; \
	DB_PATH="$${APP_DB_PATH:-./data/linkedin-cron.db}"; \
	mkdir -p "$$(dirname "$$DB_PATH")"; \
	go run ./cmd/server

run-scheduler:
	@set -a; [ -f .env ] && source .env; set +a; \
	DB_PATH="$${APP_DB_PATH:-./data/linkedin-cron.db}"; \
	mkdir -p "$$(dirname "$$DB_PATH")"; \
	go run ./cmd/scheduler

test:
	go test ./...

fmt:
	go fmt ./...

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not found; skipping"; \
	fi

clean:
	rm -rf bin
