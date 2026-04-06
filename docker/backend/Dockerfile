# Build stage
FROM golang:alpine AS builder
ENV GOTOOLCHAIN=auto

# 1. Pre-install tools and WhaTap agent (Pinned versions and consolidated)
RUN apk add --no-cache git wget tar upx && \
    wget -q https://s3.ap-northeast-2.amazonaws.com/repo.whatap.io/alpine/x86_64/whatap-agent.tar.gz && \
    tar -xzf whatap-agent.tar.gz -C / && \
    ls -R /usr/whatap/agent/

# Pin whatap-go-inst to a specific version to avoid network checks and ensure stability
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install github.com/whatap/go-api-inst/cmd/whatap-go-inst@v0.5.4

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# 2. Build and compress (Explicitly copy backend source)
COPY ai/ ./ai/
COPY auth/ ./auth/
COPY channels/ ./channels/
COPY cmd/ ./cmd/
COPY config/ ./config/
COPY handlers/ ./handlers/
COPY internal/ ./internal/
COPY logger/ ./logger/
COPY scanner/ ./scanner/
COPY services/ ./services/
COPY store/ ./store/
COPY types/ ./types/
COPY main.go ./

# Copy required config/meta files for the final stage
COPY RELEASE_NOTES_*.md whatap.conf entrypoint.sh security.conf ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux whatap-go-inst go build -ldflags="-s -w" -o message-consolidator . && \
    upx -1 message-consolidator

# 3. Final staging preparation
RUN mkdir -p /stage/app && \
    mv message-consolidator /stage/app/ && \
    # Explicitly copy only required config/meta files to the final stage
    cp RELEASE_NOTES_*.md whatap.conf entrypoint.sh security.conf /stage/app/ && \
    chmod +x /stage/app/entrypoint.sh

# Final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata libc6-compat && \
    mkdir -p /usr/whatap/agent

# Layer separation for better caching: WhaTap (stable) vs App (frequent)
COPY --from=builder /usr/whatap/ /usr/whatap/
COPY --from=builder /stage/app/ /app/

WORKDIR /app
RUN ln -sf /app/whatap.conf /usr/whatap/agent/whatap.conf

VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["./entrypoint.sh"]
