#!/usr/bin/env bash
# exit on error
set -o errexit

# Build the Go server
echo "Building Go server..."
go build -o streamvault cmd/server/main.go

# Download FFMPEG static build if it doesn't exist
if [ ! -f "ffmpeg" ]; then
    echo "Downloading FFMPEG static binaries..."
    curl -O -s https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-amd64-static.tar.xz
    tar -xf ffmpeg-release-amd64-static.tar.xz
    mv ffmpeg-*-amd64-static/ffmpeg .
    mv ffmpeg-*-amd64-static/ffprobe .
    rm -rf ffmpeg-release-amd64-static.tar.xz ffmpeg-*-amd64-static
    echo "FFMPEG downloaded successfully!"
fi
