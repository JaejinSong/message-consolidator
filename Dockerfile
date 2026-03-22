# Build stage
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git upx wget tar

# Install minify tool
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install github.com/tdewolff/minify/v2/cmd/minify@latest && \
    go install github.com/whatap/go-api-inst/cmd/whatap-go-inst@latest

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Pre-download and extract WhaTap Agent in builder to keep final image clean
RUN wget -qO- https://s3.ap-northeast-2.amazonaws.com/repo.whatap.io/alpine/x86_64/whatap-agent.tar.gz | tar -xz -C /tmp

# Copy source code
COPY . .

# Minify static files
RUN minify -r -o static-min/ static/

# Build and compress application
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux whatap-go-inst go build -ldflags="-s -w" -o message-consolidator . && \
    upx -1 message-consolidator

# Final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata libc6-compat
WORKDIR /app

# Copy WhaTap Agent from builder
COPY --from=builder /tmp/usr/whatap /usr/whatap

# Copy application artifacts
COPY --from=builder /app/message-consolidator .
COPY --from=builder /app/static-min ./static
COPY RELEASE_NOTES_USER.md whatap.conf entrypoint.sh ./
RUN chmod +x entrypoint.sh && \
    mkdir -p /usr/whatap/agent && \
    ln -s /app/whatap.conf /usr/whatap/agent/whatap.conf

VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["./entrypoint.sh"]
