package session

import (
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/securecookie"
)

const cookieName = "insighta_session"

// Data is what we encrypt and store inside the HTTP-only session cookie.
type Data struct {
	UserID       string    `json:"user_id"`
	Username     string    `json:"username"`
	Role         string    `json:"role"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"` // access token expiry
}

var ErrNoSession = errors.New("no active session")

var sc *securecookie.SecureCookie

func Init() {
	hashKey := []byte(requireEnv("SESSION_HASH_KEY"))
	blockKey := []byte(requireEnv("SESSION_BLOCK_KEY"))
	sc = securecookie.New(hashKey, blockKey)
	sc.MaxAge(60 * 60 * 24) // 24 h cookie lifetime
}

// Get decodes the session cookie into a Data struct.
func Get(r *http.Request) (*Data, error) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return nil, ErrNoSession
	}
	var d Data
	if err := sc.Decode(cookieName, cookie.Value, &d); err != nil {
		return nil, ErrNoSession
	}
	return &d, nil
}

// Set encodes d into an HTTP-only session cookie on w.
func Set(w http.ResponseWriter, d *Data) error {
	encoded, err := sc.Encode(cookieName, d)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    encoded,
		Path:     "/",
		HttpOnly: true,
		Secure:   os.Getenv("ENV") == "production",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24,
	})
	return nil
}

// Clear removes the session cookie.
func Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   os.Getenv("ENV") == "production",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic("required env var not set: " + key)
	}
	return v
}
