package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/gorilla/csrf"

	"insighta-web/internal/handlers"
	"insighta-web/internal/session"
)

func main() {
	loadEnv(".env")

	// Validate required env vars
	requiredEnv := []string{"SESSION_HASH_KEY", "SESSION_BLOCK_KEY", "CSRF_AUTH_KEY"}
	for _, k := range requiredEnv {
		if os.Getenv(k) == "" {
			log.Fatalf("missing required env var: %s", k)
		}
	}

	// Init encrypted session cookie store
	session.Init()

	// Resolve template directory — dev: relative to source, prod: relative to CWD
	tmplDir := os.Getenv("TEMPLATE_DIR")
	if tmplDir == "" {
		tmplDir = "templates"
	}
	handlers.SetTemplateDir(tmplDir)

	// CSRF protection
	csrfKey := []byte(os.Getenv("CSRF_AUTH_KEY"))
	secure := os.Getenv("ENV") == "production"
	csrfMiddleware := csrf.Protect(
		csrfKey,
		csrf.Secure(secure),
		csrf.CookieName("insighta_csrf"),
		csrf.SameSite(csrf.SameSiteLaxMode),
	)

	r := chi.NewRouter()
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(30 * time.Second))
	r.Use(csrfMiddleware)

	// Static assets
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "static"
	}
	fs := http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir)))
	r.Handle("/static/*", fs)

	// Public routes
	r.Get("/", handlers.LoginPage)
	r.Get("/auth/github", handlers.InitiateGithubLogin)
	r.Get("/auth/callback", handlers.GithubCallback)
	r.Post("/logout", handlers.Logout)

	// Protected routes
	r.Get("/dashboard", handlers.Dashboard)
	r.Get("/profiles", handlers.ProfilesList)
	r.Get("/profiles/export", handlers.ExportCSV)
	r.Get("/profiles/{id}", handlers.ProfileDetail)
	r.Get("/search", handlers.Search)
	r.Get("/account", handlers.AccountPage)

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"status":"ok","service":"insighta-web"}`)
	})

	port := os.Getenv("WEB_PORT")
	if port == "" {
		port = "3000"
	}

	log.Printf("insighta-web listening on :%s (secure=%v)", port, secure)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal(err)
	}
}

// loadEnv reads KEY=VALUE pairs from a file into the environment.
// Existing environment variables are not overridden.
func loadEnv(path string) {
	f, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range splitLines(string(f)) {
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		for i, c := range line {
			if c == '=' {
				k, v := line[:i], line[i+1:]
				if os.Getenv(k) == "" {
					os.Setenv(k, v)
				}
				break
			}
		}
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
