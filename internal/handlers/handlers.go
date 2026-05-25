package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"os/exec"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"

	"streamvault/internal/auth"
	"streamvault/internal/config"
	"streamvault/internal/models"
	"streamvault/internal/proxy"
)

type Handlers struct {
	DB     *pgxpool.Pool
	Config *config.Config
}

func NewHandlers(db *pgxpool.Pool, cfg *config.Config) *Handlers {
	return &Handlers{DB: db, Config: cfg}
}

// ServePage serves static HTML files from the frontend folder
func (h *Handlers) ServePage(w http.ResponseWriter, r *http.Request, filename string) {
	path := filepath.Join("frontend", filename)
	http.ServeFile(w, r, path)
}

// RegisterRoutes registers all paths to the default router or custom mux
func (h *Handlers) RegisterRoutes(mux *http.ServeMux) {
	// Static assets
	mux.Handle("/css/", http.StripPrefix("/css/", http.FileServer(http.Dir("frontend/css"))))
	mux.Handle("/js/", http.StripPrefix("/js/", http.FileServer(http.Dir("frontend/js"))))

	// Page routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		h.ServePage(w, r, "index.html")
	})
	mux.HandleFunc("/favicon.svg", func(w http.ResponseWriter, r *http.Request) {
		h.ServePage(w, r, "favicon.svg")
	})
	mux.HandleFunc("/player", auth.AuthRequiredMiddleware(h.DB, h.Config, func(w http.ResponseWriter, r *http.Request) {
		h.ServePage(w, r, "player.html")
	}))
	mux.HandleFunc("/history", auth.AuthRequiredMiddleware(h.DB, h.Config, func(w http.ResponseWriter, r *http.Request) {
		h.ServePage(w, r, "history.html")
	}))

	// Auth routes
	mux.HandleFunc("/auth/google", h.HandleGoogleLogin)
	mux.HandleFunc("/auth/google/callback", h.HandleGoogleCallback)
	mux.HandleFunc("/auth/logout", h.HandleLogout)

	// API routes (auth required)
	mux.HandleFunc("/api/config", auth.AuthRequiredMiddleware(h.DB, h.Config, h.HandleGetConfig))
	mux.HandleFunc("/api/drive/token", auth.AuthRequiredMiddleware(h.DB, h.Config, h.HandleDriveToken))
	mux.HandleFunc("/api/me", auth.AuthRequiredMiddleware(h.DB, h.Config, h.HandleGetMe))
	mux.HandleFunc("/api/stream", auth.AuthRequiredMiddleware(h.DB, h.Config, h.HandleStream))
	mux.HandleFunc("/api/drive/files", auth.AuthRequiredMiddleware(h.DB, h.Config, h.HandleDriveFiles))
	mux.HandleFunc("/api/drive/subtitles", auth.AuthRequiredMiddleware(h.DB, h.Config, h.HandleDriveSubtitles))
	mux.HandleFunc("/api/drive/tracks", auth.AuthRequiredMiddleware(h.DB, h.Config, h.HandleDriveTracks))
	mux.HandleFunc("/api/watch/start", auth.AuthRequiredMiddleware(h.DB, h.Config, h.HandleWatchStart))
	mux.HandleFunc("/api/watch/ping", auth.AuthRequiredMiddleware(h.DB, h.Config, h.HandleWatchPing))
	mux.HandleFunc("/api/watch/end", auth.AuthRequiredMiddleware(h.DB, h.Config, h.HandleWatchEnd))
	mux.HandleFunc("/api/video/duration", auth.AuthRequiredMiddleware(h.DB, h.Config, h.HandleUpdateDuration))
	mux.HandleFunc("/api/history", auth.AuthRequiredMiddleware(h.DB, h.Config, h.HandleHistoryRouter))
}

// WriteJSON is a helper to respond with JSON format
func WriteJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// HandleGetConfig exposes safe client configurations to the frontend (Picker API)
func (h *Handlers) HandleGetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{
		"clientId":     h.Config.GoogleClientID,
		"developerKey": h.Config.GoogleAPIKey,
	})
}

// HandleDriveToken returns the active Google OAuth token
func (h *Handlers) HandleDriveToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := r.Context().Value(auth.UserContextKey).(*models.User)
	tok, err := auth.RefreshGoogleTokenIfNeeded(r.Context(), h.DB, h.Config, user.ID)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "oauth failed: " + err.Error()})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"token": tok.AccessToken})
}

// HandleGoogleLogin starts Google OAuth flow
func (h *Handlers) HandleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	oauthCfg := auth.GetGoogleOAuthConfig(h.Config)

	// Generate a random CSRF state token and store it in a short-lived cookie
	state, err := auth.GenerateRandomState()
	if err != nil {
		http.Error(w, "failed to generate state", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
		Secure:   h.Config.SessionCookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	// Force consent and offline access to ensure we get a refresh token and grant GDrive scope
	url := oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

// HandleGoogleCallback processes code exchange and sets session cookie
func (h *Handlers) HandleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code parameter", http.StatusBadRequest)
		return
	}

	// Validate CSRF state
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value == "" {
		http.Error(w, "missing state cookie", http.StatusBadRequest)
		return
	}
	queryState := r.URL.Query().Get("state")
	if queryState != stateCookie.Value {
		http.Error(w, "invalid state parameter (CSRF check failed)", http.StatusForbidden)
		return
	}
	// Clear the state cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	oauthCfg := auth.GetGoogleOAuthConfig(h.Config)
	tok, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		http.Error(w, "token exchange failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch user details from Google userinfo API
	client := oauthCfg.Client(ctx, tok)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		http.Error(w, "failed to get user info: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	var googleUser struct {
		ID      string `json:"id"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&googleUser); err != nil {
		http.Error(w, "failed to parse user info", http.StatusInternalServerError)
		return
	}

	// Store or update User record in Postgres
	var dbUserID string
	userQuery := `
		INSERT INTO users (google_id, email, name, avatar_url)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (google_id) DO UPDATE
		SET email = EXCLUDED.email, name = EXCLUDED.name, avatar_url = EXCLUDED.avatar_url, updated_at = CURRENT_TIMESTAMP
		RETURNING id
	`
	err = h.DB.QueryRow(ctx, userQuery, googleUser.ID, googleUser.Email, googleUser.Name, googleUser.Picture).Scan(&dbUserID)
	if err != nil {
		http.Error(w, "database error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Encrypt token credentials for storing in oauth_tokens
	encAccess, err := auth.Encrypt(tok.AccessToken, h.Config.OAuthEncryptionKey)
	if err != nil {
		http.Error(w, "encryption failure: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// In OAuth callback we might get a refresh token.
	encRefresh := ""
	if tok.RefreshToken != "" {
		encRefresh, err = auth.Encrypt(tok.RefreshToken, h.Config.OAuthEncryptionKey)
		if err != nil {
			http.Error(w, "encryption failure: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	oauthQuery := `
		INSERT INTO oauth_tokens (user_id, access_token, refresh_token, expiry, scope)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id) DO UPDATE
		SET access_token = EXCLUDED.access_token,
			refresh_token = CASE WHEN EXCLUDED.refresh_token <> '' THEN EXCLUDED.refresh_token ELSE oauth_tokens.refresh_token END,
			expiry = EXCLUDED.expiry,
			scope = EXCLUDED.scope,
			updated_at = CURRENT_TIMESTAMP
	`
	_, err = h.DB.Exec(ctx, oauthQuery, dbUserID, encAccess, encRefresh, tok.Expiry, strings.Join(oauthCfg.Scopes, " "))
	if err != nil {
		http.Error(w, "token storage failure: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Establish session
	sessionToken, err := auth.CreateSession(ctx, h.DB, dbUserID, 7*24*time.Hour) // 7 days session
	if err != nil {
		http.Error(w, "session generation failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	auth.SetSessionCookie(w, sessionToken, time.Now().Add(7*24*time.Hour), h.Config.SessionCookieSecure)
	http.Redirect(w, r, "/player", http.StatusSeeOther)
}

// HandleLogout clears user session
func (h *Handlers) HandleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_token")
	if err == nil {
		tokenHash := auth.HashToken(cookie.Value)
		_, _ = h.DB.Exec(r.Context(), "DELETE FROM sessions WHERE token_hash = $1", tokenHash)
	}
	auth.ClearSessionCookie(w, h.Config.SessionCookieSecure)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// HandleGetMe returns information about the current user (sanitized — no internal IDs)
func (h *Handlers) HandleGetMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := r.Context().Value(auth.UserContextKey).(*models.User)
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"id":         user.ID,
		"name":       user.Name,
		"email":      user.Email,
		"avatar_url": user.AvatarURL,
	})
}

// HandleStream handles range requests and proxies binary streams
func (h *Handlers) HandleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := r.Context().Value(auth.UserContextKey).(*models.User)
	source := r.URL.Query().Get("source")
	_ = user // user validated by middleware

	switch source {
	case "gdrive":
		fileId := r.URL.Query().Get("fileId")
		if fileId == "" {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "fileId is required"})
			return
		}

		// Validate fileId format (alphanumeric, hyphens, underscores)
		if !isValidGDriveID(fileId) {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid fileId format"})
			return
		}

		// Ensure we have active token
		tok, err := auth.RefreshGoogleTokenIfNeeded(r.Context(), h.DB, h.Config, user.ID)
		if err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "gdrive auth refresh failed: " + err.Error()})
			return
		}

		targetURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?alt=media&supportsAllDrives=true", fileId)
		headers := map[string]string{
			"Authorization": "Bearer " + tok.AccessToken,
		}

		audioTrack := r.URL.Query().Get("audioTrack")
		trackIndex := r.URL.Query().Get("trackIndex")
		isSub := r.URL.Query().Get("isSub") == "true"

		if isSub {
			err = proxy.SubProxy(w, r, targetURL, headers, trackIndex)
		} else if audioTrack != "" {
			err = proxy.AudioRemuxProxy(w, r, targetURL, headers, audioTrack)
		} else {
			err = proxy.StreamProxy(w, r, targetURL, headers)
		}

		if err != nil {
			// If proxy returned an error without writing to client, write JSON error
			if !strings.Contains(err.Error(), "error during stream piping") {
				WriteJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			}
			return
		}

	case "terabox":
		targetURL := r.URL.Query().Get("url")
		if targetURL == "" {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
			return
		}

		// Extract the real direct stream link from Terabox
		realStreamURL, err := proxy.ExtractTeraboxStreamURL(targetURL)
		if err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "terabox link extraction failed: " + err.Error()})
			return
		}

		headers := map[string]string{
			"Referer":    "https://www.terabox.com/",
			"User-Agent": proxy.UserAgent,
		}

		err = proxy.StreamProxy(w, r, realStreamURL, headers)
		if err != nil {
			return
		}

	case "direct":
		targetURL := r.URL.Query().Get("url")
		if targetURL == "" {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
			return
		}

		// Validate URL format and protect against SSRF (Internal IP access)
		u, err := url.Parse(targetURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid url"})
			return
		}
		
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" || 
			strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "169.254.") {
			WriteJSON(w, http.StatusForbidden, map[string]string{"error": "access to internal network is forbidden"})
			return
		}

		err = proxy.StreamProxy(w, r, targetURL, nil)
		if err != nil {
			if !strings.Contains(err.Error(), "error during stream piping") {
				WriteJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			}
			return
		}

	default:
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid source type"})
	}
}

// isValidGDriveID validates that a Google Drive file/folder ID is well-formed
func isValidGDriveID(id string) bool {
	// GDrive IDs are alphanumeric with hyphens and underscores, 10-60 chars
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]{10,60}$`, id)
	return matched
}

// HandleDriveFiles lists folders and video files in the specified folderId from GDrive
func (h *Handlers) HandleDriveFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := r.Context().Value(auth.UserContextKey).(*models.User)
	folderID := r.URL.Query().Get("folderId")
	if folderID == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "folderId is required"})
		return
	}

	// Validate folderId format
	if !isValidGDriveID(folderID) {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid folderId format"})
		return
	}

	tok, err := auth.RefreshGoogleTokenIfNeeded(r.Context(), h.DB, h.Config, user.ID)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "oauth failed: " + err.Error()})
		return
	}

	// GDrive query API: files in parents and not trashed
	queryStr := fmt.Sprintf("'%s' in parents and trashed = false and (mimeType contains 'video/' or mimeType = 'application/vnd.google-apps.folder')", folderID)
	apiURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files?q=%s&fields=files(id,name,mimeType,size,thumbnailLink,videoMediaMetadata)&pageSize=100&supportsAllDrives=true", url.QueryEscape(queryStr))

	req, err := http.NewRequestWithContext(r.Context(), "GET", apiURL, nil)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		WriteJSON(w, resp.StatusCode, map[string]string{"error": string(body)})
		return
	}

	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	WriteJSON(w, http.StatusOK, result)
}

// HandleDriveSubtitles looks for SRT or VTT subtitle files in the same folder as the video file
func (h *Handlers) HandleDriveSubtitles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := r.Context().Value(auth.UserContextKey).(*models.User)
	folderID := r.URL.Query().Get("folderId")
	fileID := r.URL.Query().Get("fileId")

	if folderID == "" && fileID == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "folderId or fileId is required"})
		return
	}

	tok, err := auth.RefreshGoogleTokenIfNeeded(r.Context(), h.DB, h.Config, user.ID)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "oauth failed: " + err.Error()})
		return
	}

	if fileID != "" && folderID == "" {
		parentReqURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?fields=parents", fileID)
		req, _ := http.NewRequestWithContext(r.Context(), "GET", parentReqURL, nil)
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			var parentData struct {
				Parents []string `json:"parents"`
			}
			json.NewDecoder(resp.Body).Decode(&parentData)
			resp.Body.Close()
			if len(parentData.Parents) > 0 {
				folderID = parentData.Parents[0]
			}
		}
	}

	if folderID == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "could not determine folderId"})
		return
	}

	// Filter files with .srt or .vtt in name/mimeType
	queryStr := fmt.Sprintf("'%s' in parents and trashed = false and (name contains '.srt' or name contains '.vtt')", folderID)
	apiURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files?q=%s&fields=files(id,name,mimeType)", url.QueryEscape(queryStr))

	req, err := http.NewRequestWithContext(r.Context(), "GET", apiURL, nil)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		WriteJSON(w, resp.StatusCode, map[string]string{"error": string(body)})
		return
	}

	var result struct {
		Files []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			MimeType string `json:"mimeType"`
		} `json:"files"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	WriteJSON(w, http.StatusOK, result.Files)
}

// HandleDriveTracks uses ffprobe to inspect a Google Drive video stream and return available audio and subtitle tracks
func (h *Handlers) HandleDriveTracks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := r.Context().Value(auth.UserContextKey).(*models.User)
	fileID := r.URL.Query().Get("fileId")
	if fileID == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "fileId is required"})
		return
	}

	tok, err := auth.RefreshGoogleTokenIfNeeded(r.Context(), h.DB, h.Config, user.ID)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "oauth failed: " + err.Error()})
		return
	}

	targetURL := fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?alt=media&supportsAllDrives=true", fileID)

	cmd := exec.CommandContext(r.Context(), proxy.GetExecutablePath("ffprobe"),
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-headers", "Authorization: Bearer "+tok.AccessToken+"\r\n",
		targetURL)

	out, err := cmd.Output()
	if err != nil {
		fmt.Println("ffprobe error or not found:", err)
		WriteJSON(w, http.StatusOK, map[string]interface{}{"audio": []interface{}{}, "subtitles": []interface{}{}})
		return
	}

	var probeData struct {
		Streams []struct {
			Index     int    `json:"index"`
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Tags      struct {
				Language string `json:"language"`
				Title    string `json:"title"`
			} `json:"tags"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(out, &probeData); err != nil {
		fmt.Println("failed to parse ffprobe output:", err)
		WriteJSON(w, http.StatusOK, map[string]interface{}{"audio": []interface{}{}, "subtitles": []interface{}{}})
		return
	}

	type Track struct {
		Index    int    `json:"index"`
		Language string `json:"language"`
		Title    string `json:"title"`
		Codec    string `json:"codec"`
	}

	audioTracks := []Track{}
	subtitleTracks := []Track{}

	for _, stream := range probeData.Streams {
		t := Track{
			Index:    stream.Index,
			Language: stream.Tags.Language,
			Title:    stream.Tags.Title,
			Codec:    stream.CodecName,
		}
		if stream.CodecType == "audio" {
			audioTracks = append(audioTracks, t)
		} else if stream.CodecType == "subtitle" {
			subtitleTracks = append(subtitleTracks, t)
		}
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"audio":     audioTracks,
		"subtitles": subtitleTracks,
	})
}

// HandleUpdateDuration updates the video's duration_seconds after metadata loads on the frontend
func (h *Handlers) HandleUpdateDuration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := r.Context().Value(auth.UserContextKey).(*models.User)

	var reqBody struct {
		VideoID         string `json:"video_id"`
		DurationSeconds int    `json:"duration_seconds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	if reqBody.VideoID == "" || reqBody.DurationSeconds <= 0 {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "video_id and positive duration_seconds are required"})
		return
	}

	// Only update if the video belongs to the current user and existing duration is 0
	ctx := r.Context()
	updateQuery := `
		UPDATE videos
		SET duration_seconds = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND user_id = $3 AND duration_seconds = 0
	`
	_, err := h.DB.Exec(ctx, updateQuery, reqBody.DurationSeconds, reqBody.VideoID, user.ID)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update duration"})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleWatchStart creates or loads a video in DB, then starts a tracking watch_session
func (h *Handlers) HandleWatchStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := r.Context().Value(auth.UserContextKey).(*models.User)

	var reqBody struct {
		Title           string            `json:"video_title"`
		SourceType      models.SourceType `json:"source_type"`
		SourceID        string            `json:"source_id"`
		ThumbnailURL    string            `json:"thumbnail_url"`
		DurationSeconds int               `json:"duration_seconds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	if reqBody.SourceID == "" || reqBody.Title == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "source_id and video_title are required"})
		return
	}

	// 1. Get or create video entry
	ctx := r.Context()
	var videoID string

	// Try to find existing
	findQuery := `SELECT id FROM videos WHERE user_id = $1 AND source_type = $2 AND source_id = $3`
	err := h.DB.QueryRow(ctx, findQuery, user.ID, reqBody.SourceType, reqBody.SourceID).Scan(&videoID)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Create new
			insertQuery := `
				INSERT INTO videos (user_id, title, source_type, source_id, thumbnail_url, duration_seconds)
				VALUES ($1, $2, $3, $4, $5, $6)
				RETURNING id
			`
			err = h.DB.QueryRow(ctx, insertQuery, user.ID, reqBody.Title, reqBody.SourceType, reqBody.SourceID, reqBody.ThumbnailURL, reqBody.DurationSeconds).Scan(&videoID)
			if err != nil {
				WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save video: " + err.Error()})
				return
			}
		} else {
			WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	// 2. Fetch last watched position to return for resume
	var lastPositionSeconds int
	posQuery := `
		SELECT last_position_seconds 
		FROM watch_sessions 
		WHERE user_id = $1 AND video_id = $2 
		ORDER BY updated_at DESC LIMIT 1
	`
	_ = h.DB.QueryRow(ctx, posQuery, user.ID, videoID).Scan(&lastPositionSeconds)

	// 3. Create watch session
	var sessionID string
	sessionQuery := `
		INSERT INTO watch_sessions (user_id, video_id, last_position_seconds, total_watch_seconds)
		VALUES ($1, $2, $3, 0)
		RETURNING id
	`
	err = h.DB.QueryRow(ctx, sessionQuery, user.ID, videoID, lastPositionSeconds).Scan(&sessionID)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to start watch session"})
		return
	}

	// 4. Log play event
	eventQuery := `INSERT INTO watch_events (watch_session_id, event_type, position_seconds) VALUES ($1, $2, $3)`
	_, _ = h.DB.Exec(ctx, eventQuery, sessionID, models.EventPlay, lastPositionSeconds)

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"session_id":            sessionID,
		"video_id":              videoID,
		"last_position_seconds": lastPositionSeconds,
	})
}

// HandleWatchPing updates session details every 10s
func (h *Handlers) HandleWatchPing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := r.Context().Value(auth.UserContextKey).(*models.User)

	var reqBody struct {
		SessionID       string `json:"session_id"`
		PositionSeconds int    `json:"position_seconds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	ctx := r.Context()

	// Verify ownership of watch session
	var dbUserID string
	var currentTotal int
	verifyQuery := `SELECT user_id, total_watch_seconds FROM watch_sessions WHERE id = $1`
	err := h.DB.QueryRow(ctx, verifyQuery, reqBody.SessionID).Scan(&dbUserID, &currentTotal)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			WriteJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		} else {
			WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}

	if dbUserID != user.ID {
		WriteJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	// Update watch session. Increment watch time by 10
	updateQuery := `
		UPDATE watch_sessions
		SET last_position_seconds = $1, total_watch_seconds = total_watch_seconds + 10, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
	`
	_, err = h.DB.Exec(ctx, updateQuery, reqBody.PositionSeconds, reqBody.SessionID)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update session"})
		return
	}

	// Log ping event
	eventQuery := `INSERT INTO watch_events (watch_session_id, event_type, position_seconds) VALUES ($1, $2, $3)`
	_, _ = h.DB.Exec(ctx, eventQuery, reqBody.SessionID, models.EventPing, reqBody.PositionSeconds)

	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleWatchEnd updates final states and stops logs
func (h *Handlers) HandleWatchEnd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := r.Context().Value(auth.UserContextKey).(*models.User)

	var reqBody struct {
		SessionID       string `json:"session_id"`
		PositionSeconds int    `json:"position_seconds"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
		return
	}

	ctx := r.Context()

	// Verify and pull video duration to compute completeness
	var dbUserID string
	var videoDuration int
	query := `
		SELECT ws.user_id, v.duration_seconds
		FROM watch_sessions ws
		JOIN videos v ON ws.video_id = v.id
		WHERE ws.id = $1
	`
	err := h.DB.QueryRow(ctx, query, reqBody.SessionID).Scan(&dbUserID, &videoDuration)
	if err != nil {
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	if dbUserID != user.ID {
		WriteJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	// If position represents 90%+ duration, mark completed
	completed := false
	if videoDuration > 0 && reqBody.PositionSeconds >= int(float64(videoDuration)*0.9) {
		completed = true
	}

	updateQuery := `
		UPDATE watch_sessions
		SET ended_at = CURRENT_TIMESTAMP, last_position_seconds = $1, completed = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3
	`
	_, err = h.DB.Exec(ctx, updateQuery, reqBody.PositionSeconds, completed, reqBody.SessionID)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to complete session"})
		return
	}

	// Log end event
	eventQuery := `INSERT INTO watch_events (watch_session_id, event_type, position_seconds) VALUES ($1, $2, $3)`
	_, _ = h.DB.Exec(ctx, eventQuery, reqBody.SessionID, models.EventEnd, reqBody.PositionSeconds)

	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// HandleHistoryRouter filters history lists or targets specific items
func (h *Handlers) HandleHistoryRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	// Extract sub-paths: check if path contains an ID (e.g. /api/history/video-uuid)
	pathParts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/history"), "/")

	if len(pathParts) > 1 && pathParts[1] != "" {
		videoID := pathParts[1]
		h.HandleGetVideoHistory(w, r, videoID)
		return
	}

	h.HandleListHistory(w, r)
}

// HandleListHistory fetches watch sessions sorted and paginated
func (h *Handlers) HandleListHistory(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(auth.UserContextKey).(*models.User)

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 100 {
		limit = 10
	}
	offset := (page - 1) * limit

	sortBy := r.URL.Query().Get("sort_by")

	// Whitelist-based order clause selection — prevents SQL injection and PostgreSQL group-by errors
	orderClauses := map[string]string{
		"last_watched":    "last_watched DESC",
		"most_watched":    "watch_count DESC, last_watched DESC",
		"longest_session": "max_session_time DESC, last_watched DESC",
	}
	orderClause, ok := orderClauses[sortBy]
	if !ok {
		orderClause = "last_watched DESC" // default
	}

	ctx := r.Context()

	// Query aggregate history details
	query := fmt.Sprintf(`
		SELECT 
			v.id AS video_id,
			v.title,
			v.source_type,
			v.source_id,
			v.thumbnail_url,
			v.duration_seconds,
			MAX(ws.updated_at) AS last_watched,
			MIN(ws.started_at) AS first_watched,
			SUM(ws.total_watch_seconds) AS total_watch_time,
			COUNT(ws.id) AS watch_count,
			MAX(ws.total_watch_seconds) AS max_session_time,
			(
				SELECT last_position_seconds 
				FROM watch_sessions 
				WHERE user_id = $1 AND video_id = v.id 
				ORDER BY updated_at DESC LIMIT 1
			) AS last_position
		FROM videos v
		JOIN watch_sessions ws ON ws.video_id = v.id
		WHERE v.user_id = $1
		GROUP BY v.id
		ORDER BY %s
		LIMIT $2 OFFSET $3
	`, orderClause)

	rows, err := h.DB.Query(ctx, query, user.ID, limit, offset)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type HistoryItem struct {
		VideoID           string            `json:"video_id"`
		Title             string            `json:"title"`
		SourceType        models.SourceType `json:"source_type"`
		SourceID          string            `json:"source_id"`
		ThumbnailURL      string            `json:"thumbnail_url"`
		DurationSeconds   int               `json:"duration_seconds"`
		LastWatched       time.Time         `json:"last_watched"`
		FirstWatched      time.Time         `json:"first_watched"`
		TotalWatchSeconds int               `json:"total_watch_time"`
		WatchCount        int               `json:"watch_count"`
		LastPosition      int               `json:"last_position"`
	}

	items := make([]HistoryItem, 0)
	for rows.Next() {
		var it HistoryItem
		var maxSessionTime int
		err := rows.Scan(
			&it.VideoID, &it.Title, &it.SourceType, &it.SourceID, &it.ThumbnailURL, &it.DurationSeconds,
			&it.LastWatched, &it.FirstWatched, &it.TotalWatchSeconds, &it.WatchCount, &maxSessionTime, &it.LastPosition,
		)
		if err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		items = append(items, it)
	}

	// Check for iteration errors
	if err := rows.Err(); err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"page":  page,
		"limit": limit,
		"items": items,
	})
}

// HandleGetVideoHistory gets detail events for a single video
func (h *Handlers) HandleGetVideoHistory(w http.ResponseWriter, r *http.Request, videoID string) {
	user := r.Context().Value(auth.UserContextKey).(*models.User)
	ctx := r.Context()

	// Verify video ownership
	var title string
	err := h.DB.QueryRow(ctx, "SELECT title FROM videos WHERE id = $1 AND user_id = $2", videoID, user.ID).Scan(&title)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			WriteJSON(w, http.StatusNotFound, map[string]string{"error": "video not found"})
		} else {
			WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		return
	}

	// Fetch sessions associated with this video
	query := `
		SELECT id, started_at, ended_at, last_position_seconds, total_watch_seconds, completed
		FROM watch_sessions
		WHERE user_id = $1 AND video_id = $2
		ORDER BY created_at DESC
	`
	rows, err := h.DB.Query(ctx, query, user.ID, videoID)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type SessionDetail struct {
		ID                  string     `json:"session_id"`
		StartedAt           time.Time  `json:"started_at"`
		EndedAt             *time.Time `json:"ended_at"`
		LastPositionSeconds int        `json:"last_position_seconds"`
		TotalWatchSeconds   int        `json:"total_watch_seconds"`
		Completed           bool       `json:"completed"`
	}

	sessions := make([]SessionDetail, 0)
	for rows.Next() {
		var s SessionDetail
		err := rows.Scan(&s.ID, &s.StartedAt, &s.EndedAt, &s.LastPositionSeconds, &s.TotalWatchSeconds, &s.Completed)
		if err != nil {
			WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		sessions = append(sessions, s)
	}

	// Check for iteration errors
	if err := rows.Err(); err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"video_id": videoID,
		"title":    title,
		"sessions": sessions,
	})
}
