# A Dockerfile is a step-by-step recipe that tells Docker how to build
# an application into a container image.
#
#   - A container image = a snapshot of everything needed to run the app
#   - A Dockerfile = the instructions to create that snapshot
#
# This file uses a "multi-stage build":
#   Stage 1 (builder): uses a full Go environment to compile the code
#   Stage 2 (final):   copies only the compiled binary into a tiny image

# ── Stage 1: Build ────────────────────────────────────────────────────────────

# Start from the official Go image (has Go pre-installed)
FROM golang:1.21-alpine AS builder

# Set the working directory inside the container
WORKDIR /build

# Copy go.mod and go.sum first (dependency list)
# Docker caches layers — copying these separately means dependencies
# are only re-downloaded when go.mod changes, not on every code change
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Build the server binary
# CGO_ENABLED=0 means "no C dependencies" — makes the binary self-contained
# -o /build/mangahub-server means "output the binary to this path"
RUN CGO_ENABLED=0 GOOS=linux go build -o /build/mangahub-server ./cmd/server/main.go

# ── Stage 2: Run ──────────────────────────────────────────────────────────────

# Start from a minimal Alpine Linux image (only ~5MB)
FROM alpine:latest

# Install ca-certificates (needed for HTTPS calls to external APIs)
RUN apk --no-cache add ca-certificates wget

# Set working directory in the final image
WORKDIR /app

# Copy only the compiled binary from stage 1
COPY --from=builder /build/mangahub-server .

# Copy the data folder (manga JSON seed files)
COPY data/ ./data/

# Tell Docker which ports this container will use
# (This is documentation — the actual port binding is in docker-compose.yml)
EXPOSE 8080 9090 9091 9092 9093

# The command to run when the container starts
CMD ["./mangahub-server"]