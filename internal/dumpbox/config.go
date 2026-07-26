package dumpbox

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	defaultMaxRequestBytes    = 5 << 30
	defaultMaxFileBytes       = 5 << 30
	defaultMaxBytesPerUser    = 20 << 30
	defaultMaxFilesPerRequest = 100
	defaultUploadsPerSubject  = 4
	defaultUploadsTotal       = 32
)

type Config struct {
	ListenAddr         string
	BaseURL            *url.URL
	DataDir            string
	OIDCIssuer         string
	ClientID           string
	ClientSecret       string
	SessionKey         []byte
	MaxRequestBytes    int64
	MaxFileBytes       int64
	MaxBytesPerUser    int64
	MaxFilesPerRequest int
	MaxUploadsPerUser  int
	MaxUploadsTotal    int
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
	if err := validateIssuer(config.OIDCIssuer, os.Getenv("OIDC_ALLOW_INSECURE_ISSUER") == "true"); err != nil {
		return Config{}, err
	}

	if config.MaxRequestBytes, err = envBytes("MAX_REQUEST_BYTES", defaultMaxRequestBytes); err != nil {
		return Config{}, err
	}
	if config.MaxFileBytes, err = envBytes("MAX_FILE_BYTES", defaultMaxFileBytes); err != nil {
		return Config{}, err
	}
	if config.MaxBytesPerUser, err = envOptionalBytes("MAX_BYTES_PER_USER", defaultMaxBytesPerUser); err != nil {
		return Config{}, err
	}
	if config.MaxFilesPerRequest, err = envCount("MAX_FILES_PER_REQUEST", defaultMaxFilesPerRequest); err != nil {
		return Config{}, err
	}
	if config.MaxUploadsPerUser, err = envCount("MAX_CONCURRENT_UPLOADS_PER_USER", defaultUploadsPerSubject); err != nil {
		return Config{}, err
	}
	if config.MaxUploadsTotal, err = envCount("MAX_CONCURRENT_UPLOADS", defaultUploadsTotal); err != nil {
		return Config{}, err
	}
	if config.MaxUploadsPerUser > config.MaxUploadsTotal {
		return Config{}, errors.New("MAX_CONCURRENT_UPLOADS_PER_USER must not exceed MAX_CONCURRENT_UPLOADS")
	}
	return config, nil
}

// validateIssuer rejects issuer URLs that could expose OIDC traffic to an
// on-path attacker. Plaintext HTTP is permitted only for loopback development
// hosts, and only when explicitly enabled.
func validateIssuer(value string, allowInsecure bool) error {
	issuer, err := url.Parse(value)
	if err != nil || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return errors.New("OIDC_ISSUER_URL must be an absolute HTTPS URL without userinfo, query, or fragment")
	}
	switch issuer.Scheme {
	case "https":
		return nil
	case "http":
		if allowInsecure && isLoopbackHost(issuer.Hostname()) {
			return nil
		}
		return errors.New("OIDC_ISSUER_URL must use https; plaintext http is allowed only for loopback issuers with OIDC_ALLOW_INSECURE_ISSUER=true")
	default:
		return errors.New("OIDC_ISSUER_URL must be an absolute HTTPS URL without userinfo, query, or fragment")
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
}

func envBytes(name string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("%s must be a positive number of bytes", name)
	}
	return value, nil
}

func envOptionalBytes(name string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a non-negative number of bytes", name)
	}
	return value, nil
}

func envCount(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
