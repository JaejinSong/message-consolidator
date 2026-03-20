# Build stage
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git upx

# Install minify tool with cache (Independent of project dependencies)
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install github.com/tdewolff/minify/v2/cmd/minify@latest

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Minify static files
COPY static/ ./static/
RUN minify -r -o static-min/ static/

# Build application
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o message-consolidator . && \
    upx -1 message-consolidator

# Final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata libc6-compat
WORKDIR /app
COPY --from=builder /app/message-consolidator .
COPY --from=builder /app/static-min ./static
COPY RELEASE_NOTES_USER.md .
VOLUME ["/data"]
EXPOSE 8080
CMD ["./message-consolidator"]
