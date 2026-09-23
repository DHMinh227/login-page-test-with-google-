package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"garden-guardians-login/internal/webapp"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if err := loadEnvFile(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load .env: %w", err)
	}

	appURL := envOr("APP_URL", "http://localhost:8080")
	listenAddr := envOr("LISTEN_ADDR", ":8080")
	cookieSecure, err := strconv.ParseBool(envOr("COOKIE_SECURE", "false"))
	if err != nil {
		return fmt.Errorf("COOKIE_SECURE must be true or false: %w", err)
	}

	ctx := context.Background()
	app, err := webapp.New(ctx, webapp.Config{
		AppURL:             strings.TrimRight(appURL, "/"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		CookieSecure:       cookieSecure,
	})
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stop
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	log.Printf("Garden Guardians login is running at %s", appURL)
	if !app.GoogleConfigured() {
		log.Printf("Google login is not configured yet; copy .env.example to .env and add your client credentials")
	}

	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

// loadEnvFile intentionally supports only the simple KEY=VALUE format used by
// this template. Existing process environment variables always win.
func loadEnvFile(path string) error {
	file, err := os.Open(path)
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
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("invalid line %q", line)
		}
		name = strings.TrimSpace(name)
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		if name == "" {
			return fmt.Errorf("empty environment variable name")
		}
		if _, exists := os.LookupEnv(name); !exists {
			if err := os.Setenv(name, value); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
