# Build stage
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o chat-analyzer .

# Final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata libc6-compat
WORKDIR /app
COPY --from=builder /app/chat-analyzer .
COPY static ./static
VOLUME ["/data"]
EXPOSE 8080
CMD ["./chat-analyzer"]
