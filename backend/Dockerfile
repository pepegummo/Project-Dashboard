# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o server   ./cmd/server/main.go \
 && go build -o backfill ./cmd/backfill/main.go

# ── Stage 2: Run ──────────────────────────────────────────────────────────────
FROM alpine:latest
WORKDIR /app

# wget is required by the docker-compose healthcheck
RUN apk add --no-cache wget \
    && addgroup -S appgroup && adduser -S appuser -G appgroup

COPY --from=builder /app/server   .
COPY --from=builder /app/backfill .
RUN chown -R appuser:appgroup /app

USER appuser

EXPOSE 4000
CMD ["./server"]
