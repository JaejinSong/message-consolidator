# Build stage
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o chat-analyzer .

# Final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata libc6-compat
WORKDIR /app
COPY --from=builder /app/chat-analyzer .
COPY static ./static
VOLUME ["/data"]
EXPOSE 8080
CMD ["./chat-analyzer"]
