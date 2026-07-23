package dumpbox

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	ListenAddr   string
	BaseURL      *url.URL
	DataDir      string
	OIDCIssuer   string
	ClientID     string
	ClientSecret string
	SessionKey   []byte
}

func LoadConfig() (Config, error) {
	baseURL, err := url.Parse(env("BASE_URL", "http://localhost:8080"))
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return Config{}, errors.New("BASE_URL must be an absolute http or https URL")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	if baseURL.Path != "" {
		return Config{}, errors.New("BASE_URL must not contain a path")
	}
	baseURL.RawQuery = ""
	baseURL.Fragment = ""

	key, err := base64.StdEncoding.DecodeString(os.Getenv("SESSION_SECRET"))
	if err != nil || len(key) < 32 {
		return Config{}, errors.New("SESSION_SECRET must be base64-encoded and at least 32 bytes")
	}

	config := Config{
		ListenAddr:   env("LISTEN_ADDR", ":8080"),
		BaseURL:      baseURL,
		DataDir:      env("DATA_DIR", "./data"),
		OIDCIssuer:   strings.TrimSpace(os.Getenv("OIDC_ISSUER_URL")),
		ClientID:     strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID")),
		ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		SessionKey:   key,
	}
	if config.OIDCIssuer == "" || config.ClientID == "" || config.ClientSecret == "" {
		return Config{}, fmt.Errorf("OIDC_ISSUER_URL, OIDC_CLIENT_ID, and OIDC_CLIENT_SECRET are required")
	}
	return config, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
