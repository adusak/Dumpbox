package dumpbox

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	sessionCookie = "dumpbox_session"
	authCookie    = "dumpbox_auth"
)

type tokenVerifier interface {
	Verify(context.Context, string) (*oidc.IDToken, error)
}

type Server struct {
	baseURL      *url.URL
	dataDir      string
	oauth        oauth2.Config
	verifier     tokenVerifier
	signer       signer
	secureCookie bool
	page         *template.Template
	landingPage  *template.Template
	logger       *slog.Logger
	now          func() time.Time
}

type identityKey struct{}

func NewServer(config Config, provider *oidc.Provider, logger *slog.Logger) (*Server, error) {
	page, err := template.New("index").Parse(indexHTML)
	if err != nil {
		return nil, fmt.Errorf("parse page template: %w", err)
	}
	landingPage, err := template.New("landing").Parse(landingHTML)
	if err != nil {
		return nil, fmt.Errorf("parse landing page template: %w", err)
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		baseURL: config.BaseURL,
		dataDir: config.DataDir,
		oauth: oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  config.BaseURL.String() + "/auth/callback",
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		verifier:     provider.Verifier(&oidc.Config{ClientID: config.ClientID}),
		signer:       signer{key: config.SessionKey},
		secureCookie: config.BaseURL.Scheme == "https",
		page:         page,
		landingPage:  landingPage,
		logger:       logger,
		now:          time.Now,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /favicon.svg", brandAsset("favicon.svg"))
	mux.HandleFunc("GET /assets/logo.svg", brandAsset("logo.svg"))
	mux.HandleFunc("GET /login", s.login)
	mux.HandleFunc("GET /auth/callback", s.callback)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /", s.home)
	mux.Handle("POST /upload", s.requireAuth(http.HandlerFunc(s.upload)))
	return s.securityHeaders(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	state, err := randomValue()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	nonce, err := randomValue()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	request := authRequest{State: state, Nonce: nonce, Expires: s.now().Add(10 * time.Minute).Unix()}
	value, err := s.signer.sign(request)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.setCookie(w, authCookie, value, 10*time.Minute)
	http.Redirect(w, r, s.oauth.AuthCodeURL(state, oidc.Nonce(nonce)), http.StatusFound)
}

func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(authCookie)
	if err != nil {
		http.Error(w, "Authentication request expired. Please try again.", http.StatusBadRequest)
		return
	}
	s.clearCookie(w, authCookie)

	var request authRequest
	if s.signer.verify(cookie.Value, &request) != nil || !request.valid(s.now()) ||
		!constantTimeEqual(request.State, r.URL.Query().Get("state")) {
		http.Error(w, "Invalid authentication request. Please try again.", http.StatusBadRequest)
		return
	}
	if providerError := r.URL.Query().Get("error"); providerError != "" {
		http.Error(w, "Authentication was not completed.", http.StatusUnauthorized)
		return
	}

	token, err := s.oauth.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		s.logger.Warn("OIDC token exchange failed", "error", err)
		http.Error(w, "Authentication failed.", http.StatusUnauthorized)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "Identity provider returned no ID token.", http.StatusUnauthorized)
		return
	}
	idToken, err := s.verifier.Verify(r.Context(), rawIDToken)
	if err != nil || !constantTimeEqual(idToken.Nonce, request.Nonce) {
		s.logger.Warn("OIDC ID token verification failed", "error", err)
		http.Error(w, "Authentication failed.", http.StatusUnauthorized)
		return
	}

	var claims struct {
		Subject           string `json:"sub"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Email             string `json:"email"`
	}
	if err := idToken.Claims(&claims); err != nil || claims.Subject == "" {
		http.Error(w, "Identity provider returned invalid claims.", http.StatusUnauthorized)
		return
	}
	name := firstNonempty(claims.PreferredUsername, claims.Name, claims.Email, claims.Subject)
	userSession := session{
		Subject:  claims.Subject,
		Name:     name,
		Username: claims.PreferredUsername,
		Expires:  s.now().Add(12 * time.Hour).Unix(),
	}
	value, err := s.signer.sign(userSession)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.setCookie(w, sessionCookie, value, 12*time.Hour)
	http.Redirect(w, r, s.baseURL.Path+"/", http.StatusFound)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		http.Error(w, "Invalid request origin.", http.StatusForbidden)
		return
	}
	s.clearCookie(w, sessionCookie)
	http.Redirect(w, r, s.baseURL.Path+"/", http.StatusSeeOther)
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err == nil {
		var user session
		if s.signer.verify(cookie.Value, &user) == nil && user.valid(s.now()) {
			s.index(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, user)))
			return
		}
		s.clearCookie(w, sessionCookie)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.landingPage.Execute(w, nil); err != nil {
		s.logger.Error("render landing page", "error", err)
	}
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(identityKey{}).(session)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.page.Execute(w, struct{ Name string }{Name: user.Name}); err != nil {
		s.logger.Error("render page", "error", err)
	}
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookie)
		if err != nil {
			http.Redirect(w, r, s.baseURL.Path+"/login", http.StatusFound)
			return
		}
		var user session
		if s.signer.verify(cookie.Value, &user) != nil || !user.valid(s.now()) {
			s.clearCookie(w, sessionCookie)
			http.Redirect(w, r, s.baseURL.Path+"/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, user)))
	})
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) || r.Header.Get("X-Dumpbox-Upload") != "1" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Invalid request origin."})
		return
	}
	reader, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Expected a multipart upload."})
		return
	}
	user := r.Context().Value(identityKey{}).(session)
	directory := filepath.Join(s.dataDir, userDirectory(user))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		s.internalError(w, r, fmt.Errorf("create user directory: %w", err))
		return
	}

	var uploaded []string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Could not read the upload."})
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			_ = part.Close()
			continue
		}
		name, err := storePart(directory, part)
		_ = part.Close()
		if err != nil {
			s.logger.Error("store upload", "subject", user.Subject, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not store the file."})
			return
		}
		uploaded = append(uploaded, name)
	}
	if len(uploaded) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "No files were provided."})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"files": uploaded})
}

func storePart(directory string, part *multipart.Part) (name string, err error) {
	name = safeFilename(part.FileName())
	if name == "" {
		return "", errors.New("invalid filename")
	}
	temp, err := os.CreateTemp(directory, ".upload-*")
	if err != nil {
		return "", err
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		if err != nil {
			_ = os.Remove(tempName)
		}
	}()
	if err = temp.Chmod(0o600); err != nil {
		return "", err
	}
	buffer := make([]byte, 256*1024)
	if _, err = io.CopyBuffer(temp, part, buffer); err != nil {
		return "", err
	}
	if err = temp.Sync(); err != nil {
		return "", err
	}
	if err = temp.Close(); err != nil {
		return "", err
	}
	name, err = publishFile(directory, name, tempName)
	if err != nil {
		return "", err
	}
	if err = os.Remove(tempName); err != nil {
		return "", err
	}
	return name, nil
}

func publishFile(directory, name, tempName string) (string, error) {
	extension := filepath.Ext(name)
	stem := strings.TrimSuffix(name, extension)
	for sequence := 0; sequence < 10_000; sequence++ {
		candidate := name
		if sequence > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", stem, sequence, extension)
		}
		path := filepath.Join(directory, candidate)
		if err := os.Link(tempName, path); err == nil {
			return candidate, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", errors.New("too many files with the same name")
}

func safeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" || name == "." {
		return ""
	}
	const maxBytes = 220
	if len(name) > maxBytes {
		extension := filepath.Ext(name)
		keep := maxBytes - len(extension)
		if keep < 1 {
			extension = ""
			keep = maxBytes
		}
		name = strings.TrimSpace(name[:keep]) + extension
	}
	return name
}

func userDirectory(user session) string {
	sum := sha256Sum(user.Subject)
	username := safeDirectoryComponent(user.Username)
	if username == "" {
		return "user-" + sum[:24]
	}
	return "user-" + username + "-" + sum[:24]
}

func safeDirectoryComponent(value string) string {
	const maxBytes = 100
	var component strings.Builder
	for _, r := range strings.TrimSpace(value) {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !strings.ContainsRune("-_.@", r) {
			r = '_'
		}
		if component.Len()+utf8.RuneLen(r) > maxBytes {
			break
		}
		component.WriteRune(r)
	}
	return strings.Trim(component.String(), "._-")
}

func (s *Server) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil &&
		strings.EqualFold(parsed.Scheme, s.baseURL.Scheme) &&
		strings.EqualFold(parsed.Hostname(), s.baseURL.Hostname()) &&
		originPort(parsed) == originPort(s.baseURL)
}

func originPort(origin *url.URL) string {
	if port := origin.Port(); port != "" {
		return port
	}
	switch strings.ToLower(origin.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) setCookie(w http.ResponseWriter, name, value string, lifetime time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: s.baseURL.Path + "/", HttpOnly: true,
		Secure: s.secureCookie, SameSite: http.SameSiteLaxMode,
		MaxAge: int(lifetime.Seconds()), Expires: s.now().Add(lifetime),
	})
}

func (s *Server) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: s.baseURL.Path + "/", HttpOnly: true,
		Secure: s.secureCookie, SameSite: http.SameSiteLaxMode,
		MaxAge: -1, Expires: time.Unix(1, 0),
	})
}

func (s *Server) internalError(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	if strings.HasPrefix(r.URL.Path, "/upload") {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error."})
		return
	}
	http.Error(w, "Internal server error.", http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func randomValue() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func constantTimeEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	var result byte
	for i := range left {
		result |= left[i] ^ right[i]
	}
	return result == 0
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "user"
}
