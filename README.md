# StreamVault (Pontos-does) Cloud Video Streaming Platform

StreamVault is a premium, Netflix-grade personal cloud media streaming platform built using Go (backend), HTML5 + Vanilla JS (frontend), PostgreSQL (database), and Video.js (player framework). It enables you to securely stream video files from **Google Drive**, **Terabox**, or **direct links** in a customized interface with resume-playback capabilities and watch metrics tracking.

## Features

1. **Google OAuth2 & Picker Integration**: Authenticate with Google Drive and pick files using the secure Google Picker API. Access is limited strictly to user-selected files (`drive.file` scope).
2. **Byte Stream Proxy**: Low-memory, buffered streaming proxy (`io.CopyBuffer` with 32KB buffer) supporting HTTP Range requests for seeking and scrub performance.
3. **Terabox Extraction**: Scrapes sharing link pages to pull and proxy underlying direct media stream paths with appropriate headers.
4. **Watch History & Resume**: 10-second background tracking pings record cumulative time watched, relative timestamps, progress percentage levels, and allow immediate playback resumption.
5. **Secure Cryptography**: Access and refresh tokens are stored in the database encrypted via AES-GCM (256-bit).

---

## Tech Stack
- **Backend**: Go (Golang) — standard library `net/http` (no heavy frameworks), `pgxpool` connection pool.
- **Frontend**: HTML5 + Vanilla CSS + CSS variables (Netflix skin) + Vanilla JS + Video.js.
- **Database**: PostgreSQL.

---

## Database Setup

Initialize your PostgreSQL database and execute the migrations. The server will **automatically execute the up migration** upon startup if it detects a fresh database (i.e. the `users` table is missing).

Alternatively, you can run migrations manually:

```sql
-- To run manual setup, execute the contents of:
-- migrations/000001_init_schema.up.sql
```

---

## Setup Instructions

### 1. Google Cloud Credentials
1. Go to [Google Cloud Console](https://console.cloud.google.com/).
2. Create a project and enable:
   - **Google Drive API**
   - **Google Picker API**
3. Create OAuth 2.0 Credentials:
   - Application Type: **Web Application**
   - Authorized redirect URIs: `http://localhost:8080/auth/google/callback`
   - Save the **Client ID** and **Client Secret**.
4. Create an **API Key (Developer Key)**:
   - Navigate to Credentials -> Create Credentials -> API Key. This key is used client-side to authorize Picker overlays.

### 2. Environment Configuration
Create a `.env` file in the root directory:
```bash
cp .env.example .env
```
Fill in the values in `.env`:
- Set `DATABASE_URL` to your PostgreSQL database.
- Set Google Client ID, Secret, and Redirect URL.
- Set `GOOGLE_API_KEY` (Developer Key).
- Set `OAUTH_ENCRYPTION_KEY` to a random 32-character string.

---

## Running the Server

Initialize Go modules and run the application:
```powershell
# Get dependencies
go mod tidy

# Run the server
go run cmd/server/main.go
```
The server will bind to `http://localhost:8080`.

---

## Verification & Testing

### 1. Range Requests Verification
Validate that the byte stream proxy supports HTTP range seeking without buffering entire files:
```powershell
curl -I -H "Range: bytes=0-1023" "http://localhost:8080/api/stream?source=direct&url=https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/BigBuckBunny.mp4"
```
Ensure you receive a `206 Partial Content` status, along with `Content-Range` and `Content-Length: 1024`.

### 2. Run Automated Tests
```powershell
go test ./...
```
