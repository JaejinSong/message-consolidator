# Phase 1: Build static assets
FROM node:22-alpine AS builder

WORKDIR /app

# Optimization: Copy only package files first for better layer caching
COPY package*.json ./
RUN npm ci

# Optimization: Copy source files and set Production environment
COPY src/ ./src/
COPY static/ ./static/
COPY index.html ./
COPY *.json *.ts *.cjs ./

# Why: Pass VITE_API_BASE_URL to Vite build process for correct API endpoint resolution.
ARG VITE_API_BASE_URL=/api
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
ENV NODE_ENV=production

RUN npm run build

# Phase 2: Asset Provisioner (Sidecar)
# Why: Separating web server from assets to reduce image size and improve architecture.
FROM alpine:latest

WORKDIR /app
COPY --from=builder /app/dist /app/dist

# Why: Sync files to shared volume and keep container running if needed (or exit)
# Note: For Compose, we can use this to populate a named volume.
CMD ["sh", "-c", "echo 'Syncing assets to volume...' && cp -rv /app/dist/* /public/ && echo 'Sync complete.' && tail -f /dev/null"]
