package webapp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"time"

	"garden-guardians-login/internal/session"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	sessionCookieName = "gg_session"
	stateCookieName   = "gg_oauth_state"
	verifierCookie    = "gg_oauth_verifier"
	nonceCookie       = "gg_oauth_nonce"
)

//go:embed templates/*.html static/*.css
var assets embed.FS

type Config struct {
	AppURL             string
	GoogleClientID     string
	GoogleClientSecret string
	CookieSecure       bool
}

type App struct {
	config        Config
	templates     *template.Template
	sessions      *session.Store
	oauthConfig   *oauth2.Config
	tokenVerifier *oidc.IDTokenVerifier
	configured    bool
}

type pageData struct {
	Configured bool
	User       *session.User
	Notice     string
	Error      string
}

type googleClaims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
	Nonce         string `json:"nonce"`
}

func New(ctx context.Context, cfg Config) (*App, error) {
	if cfg.AppURL == "" {
		return nil, errors.New("APP_URL is required")
	}
	if _, err := url.ParseRequestURI(cfg.AppURL); err != nil {
		return nil, fmt.Errorf("invalid APP_URL: %w", err)
	}
	if (cfg.GoogleClientID == "") != (cfg.GoogleClientSecret == "") {
		return nil, errors.New("GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET must either both be set or both be empty")
	}

	tmpl, err := template.ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	app := &App{
		config:    cfg,
		templates: tmpl,
		sessions:  session.NewStore(12 * time.Hour),
	}

	if cfg.GoogleClientID == "" {
		return app, nil
	}

	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, fmt.Errorf("load Google OpenID configuration: %w", err)
	}
	app.oauthConfig = &oauth2.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  cfg.AppURL + "/auth/google/callback",
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
	app.tokenVerifier = provider.Verifier(&oidc.Config{ClientID: cfg.GoogleClientID})
	app.configured = true
	return app, nil
}

func (a *App) GoogleConfigured() bool { return a.configured }

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.HandleFunc("GET /", a.home)
	mux.HandleFunc("GET /auth/google/start", a.googleStart)
	mux.HandleFunc("GET /auth/google/callback", a.googleCallback)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /api/me", a.apiMe)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "google_configured": a.configured})
	})
	return securityHeaders(mux)
}

func (a *App) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := pageData{Configured: a.configured}
	if user, ok := a.currentUser(r); ok {
		data.User = &user
	}
	if r.URL.Query().Get("signed_in") == "1" {
		data.Notice = "Google sign-in succeeded. Your local game session is active."
	}
	if err := a.templates.ExecuteTemplate(w, "index.html", data); err != nil {
		log.Printf("render home: %v", err)
	}
}

func (a *App) googleStart(w http.ResponseWriter, r *http.Request) {
	if !a.configured {
		a.renderError(w, http.StatusServiceUnavailable, "Google login has not been configured yet. Add your Client ID and Client Secret to .env.")
		return
	}
	state, err := randomToken(32)
	if err != nil {
		a.renderError(w, http.StatusInternalServerError, "Could not start a secure login.")
		return
	}
	nonce, err := randomToken(32)
	if err != nil {
		a.renderError(w, http.StatusInternalServerError, "Could not start a secure login.")
		return
	}
	verifier := oauth2.GenerateVerifier()

	a.setTemporaryCookie(w, stateCookieName, state)
	a.setTemporaryCookie(w, verifierCookie, verifier)
	a.setTemporaryCookie(w, nonceCookie, nonce)

	loginURL := a.oauthConfig.AuthCodeURL(
		state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
	http.Redirect(w, r, loginURL, http.StatusFound)
}

func (a *App) googleCallback(w http.ResponseWriter, r *http.Request) {
	if !a.configured {
		a.renderError(w, http.StatusServiceUnavailable, "Google login has not been configured.")
		return
	}
	if providerError := r.URL.Query().Get("error"); providerError != "" {
		a.clearOAuthCookies(w)
		a.renderError(w, http.StatusBadRequest, "Google sign-in was cancelled or denied: "+providerError)
		return
	}

	state, err := r.Cookie(stateCookieName)
	if err != nil || !constantTimeEqual(r.URL.Query().Get("state"), state.Value) {
		a.clearOAuthCookies(w)
		a.renderError(w, http.StatusBadRequest, "The login state did not match. Please start the login again.")
		return
	}
	pkce, err := r.Cookie(verifierCookie)
	if err != nil || pkce.Value == "" {
		a.clearOAuthCookies(w)
		a.renderError(w, http.StatusBadRequest, "The login verifier was missing. Please start the login again.")
		return
	}
	nonce, err := r.Cookie(nonceCookie)
	if err != nil || nonce.Value == "" {
		a.clearOAuthCookies(w)
		a.renderError(w, http.StatusBadRequest, "The login nonce was missing. Please start the login again.")
		return
	}
	a.clearOAuthCookies(w)

	code := r.URL.Query().Get("code")
	if code == "" {
		a.renderError(w, http.StatusBadRequest, "Google did not return an authorization code.")
		return
	}
	token, err := a.oauthConfig.Exchange(r.Context(), code, oauth2.VerifierOption(pkce.Value))
	if err != nil {
		log.Printf("exchange Google authorization code: %v", err)
		a.renderError(w, http.StatusBadGateway, "Google could not complete the login. Check the server log and your callback URL.")
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		a.renderError(w, http.StatusBadGateway, "Google did not return an OpenID ID token.")
		return
	}
	idToken, err := a.tokenVerifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		log.Printf("verify Google ID token: %v", err)
		a.renderError(w, http.StatusUnauthorized, "Google returned an invalid identity token.")
		return
	}
	var claims googleClaims
	if err := idToken.Claims(&claims); err != nil {
		a.renderError(w, http.StatusUnauthorized, "Google identity information could not be read.")
		return
	}
	if !constantTimeEqual(claims.Nonce, nonce.Value) {
		a.renderError(w, http.StatusUnauthorized, "The Google identity token nonce did not match.")
		return
	}
	if claims.Subject == "" || claims.Email == "" || !claims.EmailVerified {
		a.renderError(w, http.StatusUnauthorized, "A verified Google email address is required.")
		return
	}

	sessionToken, expiresAt, err := a.sessions.Create(session.User{
		ID:      claims.Subject,
		Email:   claims.Email,
		Name:    claims.Name,
		Picture: claims.Picture,
	})
	if err != nil {
		a.renderError(w, http.StatusInternalServerError, "Could not create the local game session.")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionToken,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   a.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/?signed_in=1", http.StatusFound)
}

func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	if !a.sameOrigin(r) {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		a.sessions.Delete(cookie.Value)
	}
	a.clearCookie(w, sessionCookieName, "/")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *App) apiMe(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "not_authenticated"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "user": user})
}

func (a *App) currentUser(r *http.Request) (session.User, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return session.User{}, false
	}
	user, err := a.sessions.Get(cookie.Value)
	return user, err == nil
}

func (a *App) setTemporaryCookie(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/auth/google/callback",
		MaxAge:   10 * 60,
		HttpOnly: true,
		Secure:   a.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *App) clearOAuthCookies(w http.ResponseWriter) {
	a.clearCookie(w, stateCookieName, "/auth/google/callback")
	a.clearCookie(w, verifierCookie, "/auth/google/callback")
	a.clearCookie(w, nonceCookie, "/auth/google/callback")
}

func (a *App) clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *App) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return origin == a.config.AppURL
}

func (a *App) renderError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	if err := a.templates.ExecuteTemplate(w, "index.html", pageData{
		Configured: a.configured,
		Error:      message,
	}); err != nil {
		log.Printf("render error page: %v", err)
	}
}

func randomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https://lh3.googleusercontent.com; style-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
