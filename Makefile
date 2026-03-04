SHELL := /bin/bash

.PHONY: build run run-scheduler test clean fmt lint docker-build docker-up docker-down docker-up-ghcr import-postiz

build:
	@mkdir -p bin
	go build -o bin/stroopwafel-server ./cmd/server
	go build -o bin/stroopwafel-scheduler ./cmd/scheduler

run:
	@set -a; [ -f .env ] && source .env; set +a; \
	DB_PATH="$${APP_DB_PATH:-./data/stroopwafel.db}"; \
	mkdir -p "$$(dirname "$$DB_PATH")"; \
	go run ./cmd/server

run-scheduler:
	@set -a; [ -f .env ] && source .env; set +a; \
	DB_PATH="$${APP_DB_PATH:-./data/stroopwafel.db}"; \
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

docker-build:
	docker build -t ghcr.io/joeblack2k/stroopwafel-social-media-manager:local .

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-up-ghcr:
	./scripts/deploy-ghcr.sh

import-postiz:
	./scripts/import-postiz-linkedin.sh
