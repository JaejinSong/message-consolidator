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
COPY tsconfig.json vite.config.ts ./

# Why: Pass VITE_API_BASE_URL to Vite build process for correct API endpoint resolution.
ARG VITE_API_BASE_URL=/api
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
ENV NODE_ENV=production

RUN npm run build

# Phase 2: Asset Provisioner (Sidecar)
# Why: Separating web server from assets to reduce image size and improve architecture.
FROM alpine:3.21

WORKDIR /app
COPY --from=builder /app/dist /app/dist

CMD ["sh", "-c", "cp -r /app/dist/* /public/ && echo 'Sync complete.'"]
