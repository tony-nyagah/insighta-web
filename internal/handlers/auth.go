package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gorilla/csrf"

	"insighta-web/internal/session"
)

// LoginPage renders the login page.
func LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already logged in, redirect to dashboard
	if _, err := session.Get(r); err == nil {
		http.Redirect(w, r, "/dashboard", http.StatusFound)
		return
	}
	renderTemplate(w, r, "login.html", map[string]interface{}{
		csrf.TemplateTag: csrf.TemplateField(r),
	})
}

// InitiateGithubLogin redirects the browser to the backend OAuth entry point.
// Uses PUBLIC_API_URL when set (browser-facing), falling back to INSIGHTA_API_URL.
// This matters in Docker: INSIGHTA_API_URL points to the internal container hostname
// (e.g. http://backend:8080) which the browser cannot resolve.
func InitiateGithubLogin(w http.ResponseWriter, r *http.Request) {
	apiURL := os.Getenv("PUBLIC_API_URL")
	if apiURL == "" {
		apiURL = os.Getenv("INSIGHTA_API_URL")
	}
	if apiURL == "" {
		apiURL = "https://api.insighta.app"
	}
	http.Redirect(w, r, apiURL+"/auth/github", http.StatusFound)
}

// GithubCallback is hit after GitHub redirects back. The backend has already
// processed the code; we call the backend callback ourselves to get tokens,
// then store them in the session cookie.
func GithubCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	apiURL := os.Getenv("INSIGHTA_API_URL")
	if apiURL == "" {
		apiURL = "https://api.insighta.app"
	}

	// Exchange code with our backend
	payload := map[string]string{"code": code}
	b, _ := json.Marshal(payload)
	resp, err := http.Post(apiURL+"/auth/github/callback", "application/json", bytes.NewReader(b))
	if err != nil || resp.StatusCode != http.StatusOK {
		renderError(w, r, http.StatusBadGateway, "Authentication failed. Please try again.")
		return
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		User         struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &result); err != nil || result.AccessToken == "" {
		renderError(w, r, http.StatusBadGateway, "Unexpected response from authentication server.")
		return
	}

	sess := &session.Data{
		UserID:       result.User.ID,
		Username:     result.User.Username,
		Role:         result.User.Role,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresAt:    time.Now().Add(3 * time.Minute),
	}
	if err := session.Set(w, sess); err != nil {
		renderError(w, r, http.StatusInternalServerError, "Failed to create session.")
		return
	}

	http.Redirect(w, r, "/dashboard", http.StatusFound)
}

// Logout clears the session cookie and invalidates the refresh token.
func Logout(w http.ResponseWriter, r *http.Request) {
	sess, err := session.Get(r)
	if err == nil && sess.RefreshToken != "" {
		apiURL := os.Getenv("INSIGHTA_API_URL")
		if apiURL == "" {
			apiURL = "https://api.insighta.app"
		}
		b, _ := json.Marshal(map[string]string{"refresh_token": sess.RefreshToken})
		http.Post(apiURL+"/auth/logout", "application/json", bytes.NewReader(b))
	}
	session.Clear(w)
	http.Redirect(w, r, "/", http.StatusFound)
}

// AccountPage renders the account/profile page for the logged-in user.
func AccountPage(w http.ResponseWriter, r *http.Request) {
	sess := requireSession(w, r)
	if sess == nil {
		return
	}
	renderTemplate(w, r, "account.html", map[string]interface{}{
		"User":           sess,
		csrf.TemplateTag: csrf.TemplateField(r),
	})
}

// --- Web-only OAuth flow ---
// The web portal uses a simple redirect flow (no PKCE) since the callback
// happens entirely server-side inside a trusted environment.
// The backend callback endpoint accepts a plain code POST.

// buildOAuthURL constructs the GitHub OAuth URL going via our backend.
func buildOAuthURL(r *http.Request) string {
	apiURL := os.Getenv("INSIGHTA_API_URL")
	if apiURL == "" {
		apiURL = "https://api.insighta.app"
	}
	// Pass the web callback URL as the redirect
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	webCallbackURL := fmt.Sprintf("%s://%s/auth/callback", scheme, r.Host)
	params := url.Values{}
	params.Set("redirect_uri_override", webCallbackURL)
	return apiURL + "/auth/github?" + params.Encode()
}
