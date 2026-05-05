# Build stage
ARG BUILDER_IMAGE=golang:alpine
FROM ${BUILDER_IMAGE} AS builder
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
COPY db/ ./db/
COPY main.go ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o message-consolidator . && \
    upx -1 message-consolidator

# 3. Final staging preparation
RUN mkdir -p /stage/app && mv message-consolidator /stage/app/

# Final stage
FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata libc6-compat

# Explicitly copy application binary and WhaTap agent to the final stage
COPY --from=builder /stage/app/ /app/
COPY --from=builder /usr/whatap/agent/ /usr/whatap/agent/

# Why: Runtime assets are pulled in the final stage (not the builder) so that
# editing release notes / configs / entrypoint never invalidates the go build cache.
COPY whatap.conf security.conf entrypoint.sh /app/
COPY RELEASE_NOTES_*.md /app/

# WhaTap agent reads whatap.conf from its own home (whatap-agent wrapper does `cd /usr/whatap/agent`),
# so the file must live there — copying only to /app silently loses logsink settings.
RUN chmod +x /app/entrypoint.sh && \
    cp /app/whatap.conf /usr/whatap/agent/whatap.conf

WORKDIR /app

VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["./entrypoint.sh"]
