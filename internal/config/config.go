package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Port                string
	DatabaseURL         string
	GoogleClientID      string
	GoogleClientSecret  string
	GoogleRedirectURL   string
	GoogleAPIKey        string
	OAuthEncryptionKey  []byte
	SessionCookieSecure bool
}

// LoadConfig parses configuration from environment variables and optional .env file
func LoadConfig() (*Config, error) {
	// Try to load .env file if it exists
	_ = loadEnvFile(".env")

	port := getEnv("PORT", "8080")
	dbURL := getEnv("DATABASE_URL", "")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	googleClientID := getEnv("GOOGLE_CLIENT_ID", "")
	googleClientSecret := getEnv("GOOGLE_CLIENT_SECRET", "")
	googleRedirectURL := getEnv("GOOGLE_REDIRECT_URL", "")
	googleAPIKey := getEnv("GOOGLE_API_KEY", "")

	if googleClientID == "" || googleClientSecret == "" || googleRedirectURL == "" {
		return nil, fmt.Errorf("GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, and GOOGLE_REDIRECT_URL must be specified")
	}

	encryptionKeyStr := getEnv("OAUTH_ENCRYPTION_KEY", "")
	if len(encryptionKeyStr) != 32 {
		return nil, fmt.Errorf("OAUTH_ENCRYPTION_KEY must be exactly 32 characters/bytes long (got %d)", len(encryptionKeyStr))
	}

	secureCookie := getEnv("SESSION_COOKIE_SECURE", "false") == "true"

	return &Config{
		Port:                port,
		DatabaseURL:         dbURL,
		GoogleClientID:      googleClientID,
		GoogleClientSecret:  googleClientSecret,
		GoogleRedirectURL:   googleRedirectURL,
		GoogleAPIKey:        googleAPIKey,
		OAuthEncryptionKey:  []byte(encryptionKeyStr),
		SessionCookieSecure: secureCookie,
	}, nil
}

func getEnv(key, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

func loadEnvFile(filename string) error {
	exePath, err := os.Executable()
	envPath := filename
	if err == nil {
		exeDir := filepath.Dir(exePath)
		if _, err := os.Stat(filepath.Join(exeDir, filename)); err == nil {
			envPath = filepath.Join(exeDir, filename)
		}
	}

	file, err := os.Open(envPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		// Remove quotes if present
		if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
			(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
			val = val[1 : len(val)-1]
		}
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
	return scanner.Err()
}
