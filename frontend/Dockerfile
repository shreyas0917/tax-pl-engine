# syntax=docker/dockerfile:1
# Build the Vite app and serve it with nginx, which also proxies /api/ to the api service.

FROM node:24-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY . .
# Empty VITE_API_URL = same origin: the browser calls /api/... and nginx proxies it.
ARG VITE_API_URL=""
ENV VITE_API_URL=${VITE_API_URL}
RUN node node_modules/typescript/bin/tsc -b && node node_modules/vite/bin/vite.js build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD wget -qO- http://127.0.0.1/healthz || exit 1
