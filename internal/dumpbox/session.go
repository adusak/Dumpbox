package dumpbox

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type session struct {
	Subject  string `json:"sub"`
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	Expires  int64  `json:"exp"`
}

type authRequest struct {
	State   string `json:"state"`
	Nonce   string `json:"nonce"`
	Expires int64  `json:"exp"`
}

type signer struct {
	key []byte
}

func (s signer) sign(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + signature, nil
}

func (s signer) verify(token string, target any) error {
	encoded, signature, ok := strings.Cut(token, ".")
	if !ok {
		return errors.New("invalid signed value")
	}
	provided, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return errors.New("invalid signature")
	}
	mac := hmac.New(sha256.New, s.key)
	_, _ = mac.Write([]byte(encoded))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return errors.New("invalid signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return errors.New("invalid payload")
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return errors.New("invalid payload")
	}
	return nil
}

func (s session) valid(now time.Time) bool {
	return s.Subject != "" && s.Expires > now.Unix()
}

func (r authRequest) valid(now time.Time) bool {
	return r.State != "" && r.Nonce != "" && r.Expires > now.Unix()
}
