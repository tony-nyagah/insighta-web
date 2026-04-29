# ── Stage 1: Build ──────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o insighta-web ./cmd

# ── Stage 2: Runtime ─────────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/insighta-web .
COPY --from=builder /app/templates ./templates
COPY --from=builder /app/static    ./static

EXPOSE 3000

CMD ["./insighta-web"]
