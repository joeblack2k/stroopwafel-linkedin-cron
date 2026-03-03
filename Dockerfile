FROM golang:1.24-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/linkedin-cron-server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/linkedin-cron-scheduler ./cmd/scheduler

FROM debian:bookworm-slim AS runtime

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates tzdata && \
    rm -rf /var/lib/apt/lists/*

RUN useradd --system --create-home --home /app appuser

WORKDIR /app

COPY --from=build /out/linkedin-cron-server /usr/local/bin/linkedin-cron-server
COPY --from=build /out/linkedin-cron-scheduler /usr/local/bin/linkedin-cron-scheduler
COPY web ./web
COPY migrations ./migrations
COPY .env.example ./.env.example

ENV APP_ADDR=:8080
ENV APP_DB_PATH=/data/linkedin-cron.db
ENV APP_TIMEZONE=UTC
ENV PUBLISHER_MODE=dry-run

RUN mkdir -p /data && chown -R appuser:appuser /app /data

USER appuser

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/linkedin-cron-server"]
