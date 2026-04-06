# Phase 1: Build static assets
FROM node:22-alpine AS builder

WORKDIR /app

# Optimization: Copy only package files first for better layer caching
COPY package*.json ./
RUN npm ci

# Optimization: Copy source files and set Production environment
COPY src/ ./src/
COPY types/ ./types/
COPY static/ ./static/
COPY index.html ./
COPY *.json *.ts *.cjs ./

# Why: Pass VITE_API_BASE_URL to Vite build process for correct API endpoint resolution.
ARG VITE_API_BASE_URL=/api
ENV VITE_API_BASE_URL=$VITE_API_BASE_URL
ENV NODE_ENV=production

RUN npm run build

# Final stage: Caddy
FROM caddy:2-alpine

WORKDIR /usr/share/caddy
# Delete default Caddy welcome page to avoid conflicts
RUN rm -rf /usr/share/caddy/*
# Copy static files to Caddy's default root
COPY --from=builder /app/dist /usr/share/caddy
# Copy Caddy configuration
COPY Caddyfile /etc/caddy/Caddyfile

EXPOSE 80 443
