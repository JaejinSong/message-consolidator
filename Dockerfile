# Final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy pre-built binary
COPY message-consolidator-vps ./chat-analyzer
COPY static ./static

# /data is mounted as a persistent volume
VOLUME ["/data"]

EXPOSE 8080

CMD ["./chat-analyzer"]
