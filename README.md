# Pontos-Does Cloud Video Streaming Platform

Pontos-Does is a premium, Netflix-grade personal cloud media streaming platform built with Go (backend), HTML5 + Vanilla JS (frontend), PostgreSQL, and Video.js. 

It enables you to securely stream video files from cloud providers in a customized interface with real-time FFMPEG track extraction, resume-playback capabilities, and watch metrics tracking.

🌐 **Live Production Site:** [https://pontos-does.onrender.com](https://pontos-does.onrender.com)

---

## Core Features & Functions

- **Universal Cloud Streaming**: Stream directly from **Google Drive**, **Terabox**, or raw direct URLs without downloading files locally.
- **Embedded Audio & Subtitle Extraction**: Automatically probes and extracts embedded audio tracks and subtitles (SRT/VTT) on-the-fly directly from MKV/MP4 streams using a custom FFMPEG proxy pipeline.
- **Byte Stream Proxy**: Low-memory, buffered streaming proxy (`io.CopyBuffer`) supporting HTTP Range requests for instant seeking and scrubbing performance.
- **Watch History & Resume**: 
  - Background tracking pings (10s intervals) record cumulative time watched, relative timestamps, and progress.
  - One-click resume playback from your exact last position.
  - Soft-delete "Clear History" capability to manage your profile.
- **Dynamic Frontend**: Modern Netflix-inspired UI with CSS variables, Glassmorphism elements, and fully responsive layout.

## Authentication & Security

- **Google OAuth2 & Picker API**: Authenticate instantly with Google OAuth. Pick video files using the secure Google Picker API. Access is scoped strictly to user-selected files (`drive.file` scope) for privacy.
- **Military-Grade Cryptography**: Your Google OAuth access and refresh tokens are securely encrypted in the PostgreSQL database using AES-GCM (256-bit encryption).
- **Session Management**: Secure, HTTP-only, encrypted session cookies for persistent authentication.

---

## Tech Stack
- **Backend**: Go (Golang) — `net/http` standard library, `pgxpool` for PostgreSQL connections, and `os/exec` for FFMPEG bindings.
- **Frontend**: HTML5, Vanilla CSS, Vanilla JS, Video.js framework.
- **Database**: PostgreSQL (Auto-migrating schema).
- **Deployment**: Native Docker containerization running on Render.

---

## Local Development Setup

### 1. Google Cloud Credentials
1. Go to [Google Cloud Console](https://console.cloud.google.com/).
2. Create a project and enable **Google Drive API** and **Google Picker API**.
3. Create OAuth 2.0 Credentials (Web Application) and set Authorized redirect URIs to `http://localhost:8080/auth/google/callback`.
4. Create an **API Key** for the Picker overlay.

### 2. Environment Configuration
Create a `.env` file in the root directory:
```bash
cp .env.example .env
```
Fill in the required values:
- `DATABASE_URL` (Your PostgreSQL connection string)
- `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_REDIRECT_URL`
- `GOOGLE_API_KEY`
- `OAUTH_ENCRYPTION_KEY` (Random 32-character string)

### 3. Run the Server
Ensure you have FFMPEG and FFPROBE installed locally on your system path.
```bash
# Get dependencies
go mod tidy

# Run the server
go run cmd/server/main.go
```
The server will bind to `http://localhost:8080`.

---

## Deployment (Render)

This repository includes a `Dockerfile` pre-configured to install FFMPEG and CA Certificates on Debian.
1. Connect your repository to Render as a **Web Service**.
2. Set the Environment/Runtime to **Docker**.
3. Provide the necessary Environment Variables (from your `.env`).
4. Deploy! Render will automatically build the Go binary and provision the FFMPEG utilities required for audio track switching.
