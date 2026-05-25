# Build stage
FROM golang:1.23-bullseye AS builder

WORKDIR /app

# Download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application code
COPY . .

# Build the Go binary
RUN go build -o /app/server cmd/server/main.go


# Final production stage
FROM debian:bullseye-slim

WORKDIR /app

# Install FFMPEG, FFProbe, and CA Certificates (required for HTTPS/Google Drive API)
RUN apt-get update && apt-get install -y \
    ffmpeg \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy the built binary and static assets from the builder stage
COPY --from=builder /app/server /app/server
COPY --from=builder /app/frontend /app/frontend

# Copy the database migrations folder (required for DB init on boot)
COPY --from=builder /app/migrations /app/migrations

# Ensure the executable has the right permissions
RUN chmod +x /app/server

# Expose Render's default port
EXPOSE 8080

# Start the server
CMD ["/app/server"]
