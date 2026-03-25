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

# Minify static files first to leverage build cache
COPY static/ ./static/
RUN minify -r -o static-min/ static/

# Copy source code
COPY . .

# Build and compress application
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux whatap-go-inst go build -ldflags="-s -w" -o message-consolidator . && \
    upx -1 message-consolidator

# Pre-download WhaTap and create a staging area to minimize layers in the final image
RUN wget -qO- https://s3.ap-northeast-2.amazonaws.com/repo.whatap.io/alpine/x86_64/whatap-agent.tar.gz | tar -xz -C /tmp && \
    mkdir -p /stage/app /stage/usr && \
    mv /tmp/usr/whatap /stage/usr/ && \
    mv message-consolidator /stage/app/ && \
    mv static-min /stage/app/static && \
    cp RELEASE_NOTES_USER.md whatap.conf entrypoint.sh security.conf /stage/app/ && \
    chmod +x /stage/app/entrypoint.sh

# Final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata libc6-compat && \
    mkdir -p /usr/whatap/agent

# Single COPY instruction to drastically reduce image layers
COPY --from=builder /stage/ /
WORKDIR /app

RUN ln -s /app/whatap.conf /usr/whatap/agent/whatap.conf

VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["./entrypoint.sh"]
