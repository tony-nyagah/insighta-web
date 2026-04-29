package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"insighta-web/internal/session"
)

const (
	maxRetries    = 3
	retryBaseWait = 500 * time.Millisecond
)

// Client calls the backend API on behalf of a logged-in web user,
// transparently refreshing the access token when needed.
type Client struct {
	baseURL    string
	httpClient *http.Client
	sess       *session.Data
	w          http.ResponseWriter // needed to update the cookie after refresh
}

func New(w http.ResponseWriter, sess *session.Data) *Client {
	base := os.Getenv("API_URL")
	if base == "" {
		base = "https://api.insighta.app"
	}
	return &Client{
		baseURL:    base,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		sess:       sess,
		w:          w,
	}
}

func (c *Client) Get(path string) ([]byte, int, error) {
	return c.do(http.MethodGet, path, nil)
}

func (c *Client) Post(path string, body interface{}) ([]byte, int, error) {
	return c.do(http.MethodPost, path, body)
}

func (c *Client) GetRaw(path string) ([]byte, string, error) {
	if err := c.ensureFreshToken(); err != nil {
		return nil, "", err
	}
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryBaseWait << (attempt - 1))
		}
		req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
		if err != nil {
			return nil, "", err
		}
		c.setHeaders(req)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, "", err
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			continue
		}
		return raw, resp.Header.Get("Content-Disposition"), nil
	}
	return nil, "", fmt.Errorf("rate limit exceeded — please slow down and try again")
}

func (c *Client) do(method, path string, body interface{}) ([]byte, int, error) {
	if err := c.ensureFreshToken(); err != nil {
		return nil, http.StatusUnauthorized, err
	}

	raw, status, err := c.sendWithRetry(method, path, body)
	if err != nil {
		return nil, 0, err
	}
	if status == http.StatusUnauthorized {
		if rerr := c.refresh(); rerr != nil {
			return nil, status, fmt.Errorf("session expired")
		}
		raw, status, err = c.sendWithRetry(method, path, body)
	}
	return raw, status, err
}

func (c *Client) send(method, path string, body interface{}) ([]byte, int, error) {
	var br io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		br = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, br)
	if err != nil {
		return nil, 0, err
	}
	c.setHeaders(req)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return raw, resp.StatusCode, nil
}

// sendWithRetry wraps send with exponential backoff on HTTP 429 responses.
// Waits 500 ms, 1 s, 2 s between attempts before giving up.
func (c *Client) sendWithRetry(method, path string, body interface{}) ([]byte, int, error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(retryBaseWait << (attempt - 1))
		}
		raw, status, err := c.send(method, path, body)
		if err != nil || status != http.StatusTooManyRequests {
			return raw, status, err
		}
	}
	return nil, http.StatusTooManyRequests, fmt.Errorf("rate limit exceeded — please slow down and try again")
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.sess.AccessToken)
	req.Header.Set("X-API-Version", "1")
}

func (c *Client) ensureFreshToken() error {
	if time.Until(c.sess.ExpiresAt) > 10*time.Second {
		return nil
	}
	return c.refresh()
}

func (c *Client) refresh() error {
	payload := map[string]string{"refresh_token": c.sess.RefreshToken}
	b, _ := json.Marshal(payload)

	resp, err := c.httpClient.Post(c.baseURL+"/auth/refresh", "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("refresh failed")
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	c.sess.AccessToken = result.AccessToken
	c.sess.RefreshToken = result.RefreshToken
	c.sess.ExpiresAt = time.Now().Add(3 * time.Minute)

	// Persist updated tokens back into the session cookie
	return session.Set(c.w, c.sess)
}
