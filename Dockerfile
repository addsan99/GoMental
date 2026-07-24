# GoMental server image. The single binary embeds the browser SPA and runs in
# headless `serve` mode. The desktop (Wails) build is unaffected and not built here.
#
# modernc.org/sqlite is pure Go, so the binary builds with CGO disabled and runs
# on a minimal static base.

# --- Stage 1: build the browser SPA bundle -------------------------------------
FROM node:22-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci || npm install
COPY frontend/ ./
# Runtime-selected transport: an unset VITE_TRANSPORT yields a bundle that
# auto-detects Wails vs browser, which is what the server serves.
RUN node node_modules/vite/bin/vite.js build

# --- Stage 2: build the Go binary (embeds frontend/dist) -----------------------
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/frontend/dist ./frontend/dist
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/gomental .

# --- Stage 3: minimal runtime --------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/gomental /usr/local/bin/gomental
# Workspace is expected at /data (mount a volume). Override via flags/env.
ENV GOMENTAL_WORKSPACE=/data
ENV GOMENTAL_ADDR=:8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/gomental", "serve"]
