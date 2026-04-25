# Why: Backend binary is now built on the host by deploy.sh (BE: Compile) so the
# image build can reuse the host's already-warm ~/.cache/go-build. Docker's cache
# mount is isolated from the host cache, so re-compiling inside the container
# floored BE: Build at ~21s even on warm runs. Packaging-only image build: ~5–7s.
ARG BUILDER_IMAGE=golang:alpine
FROM ${BUILDER_IMAGE} AS whatap-source
# whatap agent is installed at /usr/whatap/agent/ via Dockerfile.builder.

# Final stage
FROM alpine:3.21
RUN apk --no-cache add ca-certificates tzdata libc6-compat

COPY message-consolidator /app/message-consolidator
COPY RELEASE_NOTES_*.md whatap.conf entrypoint.sh security.conf /app/
COPY --from=whatap-source /usr/whatap/agent/ /usr/whatap/agent/

# WhaTap agent reads whatap.conf from its own home (whatap-agent wrapper does `cd /usr/whatap/agent`),
# so the file must live there — copying only to /app silently loses logsink settings.
RUN cp /app/whatap.conf /usr/whatap/agent/whatap.conf && \
    chmod +x /app/entrypoint.sh

WORKDIR /app

VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["./entrypoint.sh"]
