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
COPY store/queries/ ./store/queries/
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
    mkdir -p /stage/app/store/queries && \
    cp store/queries/*.sql /stage/app/store/queries/ && \
    chmod +x /stage/app/entrypoint.sh

# Final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata libc6-compat

# Explicitly copy application files and WhaTap agent to the final stage
COPY --from=builder /stage/app/ /app/
COPY --from=builder /usr/whatap/agent/ /usr/whatap/agent/

WORKDIR /app

VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["./entrypoint.sh"]
