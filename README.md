# Insighta Web Portal

Go + HTMX server-rendered web portal for the Insighta Labs+ Profile Intelligence Platform. Sits in front of `insighta-backend` and provides a browser-based UI.

## Architecture

```
Browser ──(HTTPS)──► insighta-web (Go, chi, Go templates, HTMX)
                            │
                            │ HTTP (internal)
                            ▼
                     insighta-backend  (REST API)
                            │
                            ▼
                        SQLite DB
```

- **Sessions** — encrypted HTTP-only cookie via `gorilla/securecookie` (AES + HMAC)
- **CSRF** — `gorilla/csrf` middleware (Double-Submit Cookie pattern)
- **Auth** — GitHub OAuth delegated entirely to the backend; the web portal receives tokens and stores them in the session cookie
- **HTMX** — filter/search forms submit `HX-Request` headers so only the table fragment is swapped in, not the full page

## Running Locally

```bash
cp .env.example .env
# edit .env — set INSIGHTA_API_URL, SESSION_*, CSRF_AUTH_KEY

# run from the web/stage-3 directory so relative template/static paths resolve
go run ./cmd
```

The server listens on `WEB_PORT` (default `3000`). The backend must be running and accessible at `INSIGHTA_API_URL`.

## Configuration

| Variable | Required | Default | Description |
|---|---|---|---|
| `WEB_PORT` | No | `3000` | HTTP listen port |
| `INSIGHTA_API_URL` | No | `https://api.insighta.app` | Backend base URL |
| `SESSION_HASH_KEY` | **Yes** | — | 32-64 byte HMAC key for session cookie signing |
| `SESSION_BLOCK_KEY` | **Yes** | — | 16/24/32 byte AES key for session cookie encryption |
| `CSRF_AUTH_KEY` | **Yes** | — | 32 byte key for gorilla/csrf |
| `ENV` | No | `development` | Set to `production` for Secure cookies |
| `TEMPLATE_DIR` | No | `templates` | Path to HTML templates directory |
| `STATIC_DIR` | No | `static` | Path to static assets directory |

## Authentication Flow

```
User → GET /auth/github → backend /auth/github → GitHub OAuth
                                                      │
                    ◄──────────────────── redirect ◄──┘
                    GET /auth/callback?code=…
                          │
                          │  POST /auth/github/callback  (code)
                          ├────────────────────────────► backend
                          │◄──── {access_token, refresh_token, user}
                          │
                          │  Set encrypted session cookie
                          └► redirect to /dashboard
```

The web portal never stores credentials in the browser; they live in an encrypted server-side session cookie only.

## Routes

| Method | Path | Description |
|---|---|---|
| `GET` | `/` | Login page (redirects to `/dashboard` if logged in) |
| `GET` | `/auth/github` | Redirect to backend OAuth entry |
| `GET` | `/auth/callback` | Handle GitHub OAuth callback |
| `POST` | `/logout` | Clear session + revoke refresh token |
| `GET` | `/dashboard` | Overview stats |
| `GET` | `/profiles` | Paginated profiles list with filters |
| `GET` | `/profiles/export` | Proxy CSV export from backend |
| `GET` | `/profiles/{id}` | Single profile detail |
| `GET` | `/search` | NLP search page |
| `GET` | `/account` | Logged-in user info |
| `GET` | `/health` | Health check |
| `GET` | `/static/*` | Static assets |

## HTMX Partials

When a request arrives with `HX-Request: true`, profile list and search pages return only the inner fragment (`#profiles-container` or `#search-results`) rather than the full page. The filter form and search form are wired with `hx-get` + `hx-target` to trigger this automatically.

## Docker

```bash
docker build -t insighta-web .
docker run -p 3000:3000 \
  -e SESSION_HASH_KEY=… \
  -e SESSION_BLOCK_KEY=… \
  -e CSRF_AUTH_KEY=… \
  -e INSIGHTA_API_URL=http://backend:8080 \
  -e ENV=production \
  insighta-web
```

The Docker image runs from `/app` so the default `TEMPLATE_DIR=templates` and `STATIC_DIR=static` resolve correctly (both are copied next to the binary at build time).

## Project Structure

```
web/stage-3/
├── cmd/
│   └── main.go               Entry point, chi router, CSRF middleware
├── internal/
│   ├── client/
│   │   └── client.go         Backend API client with auto token refresh
│   ├── handlers/
│   │   ├── auth.go           Login, callback, logout, account handlers
│   │   └── profiles.go       Dashboard, profiles, search, export handlers + template helpers
│   └── session/
│       └── session.go        Encrypted session cookie (gorilla/securecookie)
├── templates/
│   ├── base.html             Layout with navbar, HTMX script, CSRF meta tag
│   ├── login.html            GitHub OAuth login button
│   ├── dashboard.html        Stats overview + quick actions
│   ├── profiles.html         Filtered profile list (HTMX filter form)
│   ├── profile.html          Single profile detail
│   ├── search.html           NLP search (HTMX search form)
│   ├── account.html          Logged-in user info
│   ├── error.html            Generic error page
│   └── partials/
│       ├── profiles_table.html  Profile table + pagination fragment
│       └── search_results.html  Search results fragment
├── static/
│   └── css/
│       └── app.css           Dark-mode CSS (no framework)
├── .env.example              Template for required env vars
├── Dockerfile                Multi-stage build (Go → alpine)
└── go.mod
```
