package auth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"streamvault/internal/config"
	"streamvault/internal/models"
)

type contextKey string

const UserContextKey contextKey = "user"

// Encrypt encrypts plain text using AES-GCM with the provided 32-byte key
func Encrypt(plaintext string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts ciphertext (base64) using AES-GCM with the provided 32-byte key
func Decrypt(cryptoText string, key []byte) (string, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(cryptoText)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, actualCiphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, actualCiphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// HashToken returns the SHA-256 hash of a string in hex
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// GetGoogleOAuthConfig builds the oauth2.Config from application config
func GetGoogleOAuthConfig(cfg *config.Config) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  cfg.GoogleRedirectURL,
		Scopes: []string{
			"https://www.googleapis.com/auth/drive.file",
			"https://www.googleapis.com/auth/userinfo.profile",
			"https://www.googleapis.com/auth/userinfo.email",
		},
		Endpoint: google.Endpoint,
	}
}

// CreateSession generates a new session and returns the raw cookie token
func CreateSession(ctx context.Context, db *pgxpool.Pool, userID string, duration time.Duration) (string, error) {
	// Generate random session token (32 bytes)
	tokenBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, tokenBytes); err != nil {
		return "", err
	}
	rawToken := base64.URLEncoding.EncodeToString(tokenBytes)
	tokenHash := HashToken(rawToken)
	expiresAt := time.Now().Add(duration)

	// Save session in DB
	query := `INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`
	_, err := db.Exec(ctx, query, userID, tokenHash, expiresAt)
	if err != nil {
		return "", err
	}

	return rawToken, nil
}

// SetSessionCookie sets a secure, HTTP-only session cookie
func SetSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    token,
		Expires:  expiresAt,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie deletes the session cookie
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Expires:  time.Unix(0, 0),
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	})
}

// GetUserFromSession fetches user info associated with the session token
func GetUserFromSession(ctx context.Context, db *pgxpool.Pool, rawToken string) (*models.User, error) {
	tokenHash := HashToken(rawToken)

	query := `
		SELECT u.id, u.google_id, u.email, u.name, u.avatar_url, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON s.user_id = u.id
		WHERE s.token_hash = $1 AND s.expires_at > $2
	`
	var u models.User
	err := db.QueryRow(ctx, query, tokenHash, time.Now()).Scan(
		&u.ID, &u.GoogleID, &u.Email, &u.Name, &u.AvatarURL, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("invalid or expired session")
		}
		return nil, err
	}

	return &u, nil
}

// RefreshGoogleTokenIfNeeded checks if user's Google OAuth token is expired/near-expiry and refreshes it silently
func RefreshGoogleTokenIfNeeded(ctx context.Context, db *pgxpool.Pool, cfg *config.Config, userID string) (*oauth2.Token, error) {
	// Retrieve current tokens
	query := `SELECT access_token, refresh_token, expiry, scope FROM oauth_tokens WHERE user_id = $1`
	var encAccess, encRefresh, scope string
	var expiry time.Time
	err := db.QueryRow(ctx, query, userID).Scan(&encAccess, &encRefresh, &expiry, &scope)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user oauth tokens: %w", err)
	}

	// Decrypt tokens
	accessToken, err := Decrypt(encAccess, cfg.OAuthEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt access token: %w", err)
	}
	refreshToken, err := Decrypt(encRefresh, cfg.OAuthEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt refresh token: %w", err)
	}

	tok := &oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expiry:       expiry,
		TokenType:    "Bearer",
	}

	// Google tokens expire in 1 hour. If less than 5 mins remain, refresh.
	if time.Until(tok.Expiry) > 5*time.Minute {
		return tok, nil
	}

	// Perform refresh
	oauthCfg := GetGoogleOAuthConfig(cfg)
	tokenSource := oauthCfg.TokenSource(ctx, tok)
	newTok, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("oauth token refresh failed: %w", err)
	}

	// Encrypt and update new tokens in DB
	encNewAccess, err := Encrypt(newTok.AccessToken, cfg.OAuthEncryptionKey)
	if err != nil {
		return nil, err
	}

	// Google refresh token is only returned on initial code exchange or if force_prompt is set.
	// If the refreshed token doesn't include a new refresh token, preserve the old one.
	refreshToStore := newTok.RefreshToken
	if refreshToStore == "" {
		refreshToStore = refreshToken
	}
	encNewRefresh, err := Encrypt(refreshToStore, cfg.OAuthEncryptionKey)
	if err != nil {
		return nil, err
	}

	updateQuery := `
		UPDATE oauth_tokens
		SET access_token = $1, refresh_token = $2, expiry = $3, updated_at = CURRENT_TIMESTAMP
		WHERE user_id = $4
	`
	_, err = db.Exec(ctx, updateQuery, encNewAccess, encNewRefresh, newTok.Expiry, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to update refreshed token in DB: %w", err)
	}

	return newTok, nil
}

// AuthRequiredMiddleware guards routes that require authentication
func AuthRequiredMiddleware(db *pgxpool.Pool, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_token")
		if err != nil {
			if r.Header.Get("Accept") == "application/json" || strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
			} else {
				http.Redirect(w, r, "/", http.StatusSeeOther)
			}
			return
		}

		user, err := GetUserFromSession(r.Context(), db, cookie.Value)
		if err != nil {
			// Clear invalid cookie
			ClearSessionCookie(w)
			if r.Header.Get("Accept") == "application/json" || strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
			} else {
				http.Redirect(w, r, "/", http.StatusSeeOther)
			}
			return
		}

		// Inject user into context
		ctx := context.WithValue(r.Context(), UserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// Helper to check if string contains specific prefix (needed for routing checks)
func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
