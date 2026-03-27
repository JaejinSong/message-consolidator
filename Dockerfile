# Build stage
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git upx wget tar

# 1. Pre-download WhaTap and tools (Indefinite caching potential)
RUN wget -qO- https://s3.ap-northeast-2.amazonaws.com/repo.whatap.io/alpine/x86_64/whatap-agent.tar.gz | tar -xz -C /tmp && \
    mkdir -p /stage/usr/whatap/agent && \
    mv /tmp/usr/whatap/agent/whatap_agent_static /stage/usr/whatap/agent/ && \
    mv /tmp/usr/whatap/agent/whatap-agent /stage/usr/whatap/agent/ && \
    upx -1 /stage/usr/whatap/agent/whatap_agent_static

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install github.com/tdewolff/minify/v2/cmd/minify@latest && \
    go install github.com/whatap/go-api-inst/cmd/whatap-go-inst@latest

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# 2. Minify static files
COPY static/ ./static/
RUN minify -r -o static-min/ static/

# 3. Build and compress (Only source code)
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux whatap-go-inst go build -ldflags="-s -w" -o message-consolidator . && \
    upx -1 message-consolidator

# 4. Final staging preparation
RUN mkdir -p /stage/app && \
    mv message-consolidator /stage/app/ && \
    mv static-min /stage/app/static && \
    cp RELEASE_NOTES_USER.md whatap.conf entrypoint.sh security.conf /stage/app/ && \
    chmod +x /stage/app/entrypoint.sh

# Final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata libc6-compat && \
    mkdir -p /usr/whatap/agent

# Layer separation for better caching: WhaTap (stable) vs App (frequent)
COPY --from=builder /stage/usr/ /usr/
COPY --from=builder /stage/app/ /app/

WORKDIR /app
RUN ln -s /app/whatap.conf /usr/whatap/agent/whatap.conf

VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["./entrypoint.sh"]
