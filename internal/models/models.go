package models

import (
	"time"
)

type SourceType string

const (
	SourceGDrive  SourceType = "gdrive"
	SourceTerabox SourceType = "terabox"
	SourceDirect  SourceType = "direct"
)

type EventType string

const (
	EventPlay  EventType = "play"
	EventPause EventType = "pause"
	EventSeek  EventType = "seek"
	EventEnd   EventType = "end"
	EventPing  EventType = "ping"
)

type User struct {
	ID        string    `json:"id"`
	GoogleID  string    `json:"google_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatar_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TokenHash string    `json:"token_hash"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type OAuthToken struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	AccessToken  string    `json:"access_token"`  // Encrypted in DB, decrypted in memory
	RefreshToken string    `json:"refresh_token"` // Encrypted in DB, decrypted in memory
	Expiry       time.Time `json:"expiry"`
	Scope        string    `json:"scope"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Video struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	Title           string     `json:"title"`
	SourceType      SourceType `json:"source_type"`
	SourceID        string     `json:"source_id"`
	ThumbnailURL    string     `json:"thumbnail_url"`
	DurationSeconds int        `json:"duration_seconds"`
	CreatedAt       time.Time  `json:"created_at"`
}

type WatchSession struct {
	ID                  string     `json:"id"`
	UserID              string     `json:"user_id"`
	VideoID             string     `json:"video_id"`
	StartedAt           time.Time  `json:"started_at"`
	EndedAt             *time.Time `json:"ended_at"`
	LastPositionSeconds int        `json:"last_position_seconds"`
	TotalWatchSeconds   int        `json:"total_watch_seconds"`
	Completed           bool       `json:"completed"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type WatchEvent struct {
	ID             string    `json:"id"`
	WatchSessionID string    `json:"watch_session_id"`
	EventType      EventType `json:"event_type"`
	Position       int       `json:"position_seconds"`
	CreatedAt      time.Time `json:"created_at"`
}
