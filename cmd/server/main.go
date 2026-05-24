package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"streamvault/internal/config"
	"streamvault/internal/db"
	"streamvault/internal/handlers"
)

func main() {
	log.Println("Starting StreamVault (Pontos-does) Server...")

	// 1. Load config
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	// 2. Init DB
	pool, err := db.InitDB(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Database connection failure: %v", err)
	}
	defer pool.Close()

	log.Println("Database connection established successfully.")

	// 3. Auto-run schema migrations if database is fresh
	if err := runMigrationsIfNeeded(pool); err != nil {
		log.Fatalf("Migration execution failed: %v", err)
	}

	// 4. Initialize Handlers & Routes
	h := handlers.NewHandlers(pool, cfg)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Start background cron to clean up expired sessions
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		ctx := context.Background()
		for {
			select {
			case <-ticker.C:
				_, err := pool.Exec(ctx, "DELETE FROM sessions WHERE expires_at < CURRENT_TIMESTAMP")
				if err != nil {
					log.Printf("Error cleaning up sessions: %v", err)
				}
			}
		}
	}()

	// Custom logging middleware
	loggingMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("--> %s %s", r.Method, r.URL.Path)

		// Set default headers for CORS and framing
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		mux.ServeHTTP(w, r)
		log.Printf("<-- %s %s completed in %v", r.Method, r.URL.Path, time.Since(start))
	})

	serverAddr := ":" + cfg.Port
	log.Printf("Server is running on http://localhost%s", serverAddr)

	server := &http.Server{
		Addr:         serverAddr,
		Handler:      loggingMux,
		WriteTimeout: 6 * time.Hour, // Streaming requests require long-lived connections
		ReadTimeout:  15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("Server stopped with error: %v", err)
	}
}

// runMigrationsIfNeeded reads the migration file and initializes the tables if 'users' table is missing
func runMigrationsIfNeeded(pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Check if users table exists
	var exists bool
	checkQuery := `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_name = 'users'
		)
	`
	err := pool.QueryRow(ctx, checkQuery).Scan(&exists)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("failed to check table existence: %w", err)
	}

	if exists {
		log.Println("Database schema already exists. Skipping auto-migration.")
		return nil
	}

	log.Println("Fresh database detected. Applying migrations...")

	migrationPath := filepath.Join("migrations", "000001_init_schema.up.sql")
	if _, err := os.Stat(migrationPath); os.IsNotExist(err) {
		exePath, _ := os.Executable()
		migrationPath = filepath.Join(filepath.Dir(exePath), "migrations", "000001_init_schema.up.sql")
	}

	migrationBytes, err := os.ReadFile(migrationPath)
	if err != nil {
		return fmt.Errorf("unable to read migration up script at %s: %w", migrationPath, err)
	}

	_, err = pool.Exec(ctx, string(migrationBytes))
	if err != nil {
		return fmt.Errorf("failed to execute migration script: %w", err)
	}

	log.Println("Schema migration successfully completed.")
	return nil
}
