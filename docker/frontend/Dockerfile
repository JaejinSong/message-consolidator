# Phase 1: Build static assets
FROM node:22-alpine AS builder

WORKDIR /app

# Optimization: Copy only package files first for better layer caching
COPY package*.json ./
RUN npm ci

# Why: Order COPYs from least- to most-frequently changed so config edits do not
# bust the src layer cache when only tsconfig/vite.config tweaks happen.
COPY tsconfig.json vite.config.ts ./
COPY index.html ./
COPY static/ ./static/
COPY src/ ./src/
COPY docs/user-guide/ ./docs/user-guide/

# Why: Pass VITE_API_BASE_URL to Vite build process for correct API endpoint resolution.
ARG VITE_API_BASE_URL=/api
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
ENV NODE_ENV=production

RUN npm run build

# Phase 2: Asset Provisioner (Sidecar)
# Why: busybox:musl (~0.9MB) over alpine (8.5MB) — sidecar only needs sh + cp, no apk needed.
FROM busybox:1.37.0-musl

WORKDIR /app
COPY --from=builder /app/dist /app/dist

CMD ["sh", "-c", "cp -r /app/dist/* /public/ && echo 'Sync complete.'"]
