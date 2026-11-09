package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// oauthState is carried in the HMAC-signed kira_oauth cookie between
// /auth/google/login and /auth/google/callback.
type oauthState struct {
	State    string `json:"s"`
	Verifier string `json:"v"`
	Nonce    string `json:"n"`
	Expires  int64  `json:"e"` // unix seconds
}

var errBadOAuthCookie = errors.New("invalid oauth cookie")

func (a *Auth) mac(payload string) []byte {
	m := hmac.New(sha256.New, a.cfg.SessionSecret)
	m.Write([]byte(OAuthCookie + "|" + payload))
	return m.Sum(nil)
}

func (a *Auth) encodeOAuthState(st oauthState) (string, error) {
	js, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(js)
	return payload + "." + base64.RawURLEncoding.EncodeToString(a.mac(payload)), nil
}

func (a *Auth) decodeOAuthState(v string) (oauthState, error) {
	var st oauthState
	payload, sig, ok := strings.Cut(v, ".")
	if !ok {
		return st, errBadOAuthCookie
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, a.mac(payload)) {
		return st, errBadOAuthCookie
	}
	js, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || json.Unmarshal(js, &st) != nil {
		return st, errBadOAuthCookie
	}
	if st.State == "" || st.Verifier == "" || st.Nonce == "" {
		return st, errBadOAuthCookie
	}
	if !a.now().Before(time.Unix(st.Expires, 0)) {
		return st, errBadOAuthCookie
	}
	return st, nil
}
