package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"

	"insighta-web/internal/client"
	"insighta-web/internal/session"
)

// Dashboard renders the main dashboard with aggregate counts.
func Dashboard(w http.ResponseWriter, r *http.Request) {
	sess := requireSession(w, r)
	if sess == nil {
		return
	}
	c := client.New(w, sess)

	// Fetch total count via profiles list (limit=1)
	raw, _, err := c.Get("/api/profiles?limit=1&page=1")
	var total, totalPages int
	if err == nil {
		var resp struct {
			Total      int `json:"total"`
			TotalPages int `json:"total_pages"`
		}
		json.Unmarshal(raw, &resp)
		total = resp.Total
		totalPages = resp.TotalPages
	}

	renderTemplate(w, r, "dashboard.html", map[string]interface{}{
		"User":       sess,
		"Total":      total,
		"TotalPages": totalPages,
	})
}

// ProfilesList handles GET /profiles — full page and HTMX partial.
func ProfilesList(w http.ResponseWriter, r *http.Request) {
	sess := requireSession(w, r)
	if sess == nil {
		return
	}

	params := r.URL.Query()
	apiParams := url.Values{}
	for _, k := range []string{"gender", "age_group", "country_id", "min_age", "max_age", "sort_by", "order", "page", "limit"} {
		if v := params.Get(k); v != "" {
			apiParams.Set(k, v)
		}
	}
	if apiParams.Get("page") == "" {
		apiParams.Set("page", "1")
	}
	if apiParams.Get("limit") == "" {
		apiParams.Set("limit", "20")
	}

	c := client.New(w, sess)
	raw, status, err := c.Get("/api/profiles?" + apiParams.Encode())

	var resp struct {
		Status     string                   `json:"status"`
		Page       int                      `json:"page"`
		Limit      int                      `json:"limit"`
		Total      int                      `json:"total"`
		TotalPages int                      `json:"total_pages"`
		Links      map[string]interface{}   `json:"links"`
		Data       []map[string]interface{} `json:"data"`
	}
	if err == nil && status == 200 {
		json.Unmarshal(raw, &resp)
	}

	page, _ := strconv.Atoi(apiParams.Get("page"))
	data := map[string]interface{}{
		"User":       sess,
		"Profiles":   resp.Data,
		"Page":       resp.Page,
		"Limit":      resp.Limit,
		"Total":      resp.Total,
		"TotalPages": resp.TotalPages,
		"PrevPage":   page - 1,
		"NextPage":   page + 1,
		"Filters":    params, // pass back so the form stays populated
		csrf.TemplateTag: csrf.TemplateField(r),
	}

	// HTMX partial request — return only the table rows fragment
	if r.Header.Get("HX-Request") == "true" {
		renderPartial(w, r, "partials/profiles_table.html", data)
		return
	}
	renderTemplate(w, r, "profiles.html", data)
}

// ProfileDetail handles GET /profiles/{id}.
func ProfileDetail(w http.ResponseWriter, r *http.Request) {
	sess := requireSession(w, r)
	if sess == nil {
		return
	}
	id := chi.URLParam(r, "id")

	c := client.New(w, sess)
	raw, status, err := c.Get("/api/profiles/" + id)
	if err != nil || status == 404 {
		renderError(w, r, http.StatusNotFound, "Profile not found.")
		return
	}

	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	json.Unmarshal(raw, &resp)

	renderTemplate(w, r, "profile.html", map[string]interface{}{
		"User":    sess,
		"Profile": resp.Data,
	})
}

// Search handles GET /search — HTMX-powered NLP search page.
func Search(w http.ResponseWriter, r *http.Request) {
	sess := requireSession(w, r)
	if sess == nil {
		return
	}

	q := r.URL.Query().Get("q")
	data := map[string]interface{}{
		"User":    sess,
		"Query":   q,
		"Results": []map[string]interface{}{},
		csrf.TemplateTag: csrf.TemplateField(r),
	}

	if q != "" {
		c := client.New(w, sess)
		raw, status, err := c.Get("/api/profiles/search?q=" + url.QueryEscape(q))
		if err == nil && status == 200 {
			var resp struct {
				Total int                      `json:"total"`
				Data  []map[string]interface{} `json:"data"`
			}
			json.Unmarshal(raw, &resp)
			data["Results"] = resp.Data
			data["Total"] = resp.Total
		}
	}

	if r.Header.Get("HX-Request") == "true" {
		renderPartial(w, r, "partials/search_results.html", data)
		return
	}
	renderTemplate(w, r, "search.html", data)
}

// ExportCSV proxies the CSV download from the backend to the browser.
func ExportCSV(w http.ResponseWriter, r *http.Request) {
	sess := requireSession(w, r)
	if sess == nil {
		return
	}

	params := r.URL.Query()
	apiParams := url.Values{"format": {"csv"}}
	for _, k := range []string{"gender", "age_group", "country_id"} {
		if v := params.Get(k); v != "" {
			apiParams.Set(k, v)
		}
	}

	c := client.New(w, sess)
	raw, disposition, err := c.GetRaw("/api/profiles/export?" + apiParams.Encode())
	if err != nil {
		renderError(w, r, http.StatusBadGateway, "Export failed.")
		return
	}

	if disposition == "" {
		disposition = fmt.Sprintf(`attachment; filename="profiles_export.csv"`)
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", disposition)
	w.Write(raw)
}

// --- template helpers ---

var tmplDir string

func SetTemplateDir(dir string) { tmplDir = dir }

// extraPartials maps page templates to partial files they embed via {{template "..."}}
var extraPartials = map[string]string{
	"profiles.html": "partials/profiles_table.html",
	"search.html":   "partials/search_results.html",
}

func renderTemplate(w http.ResponseWriter, r *http.Request, name string, data map[string]interface{}) {
	files := []string{
		filepath.Join(tmplDir, "base.html"),
		filepath.Join(tmplDir, name),
	}
	if partial, ok := extraPartials[name]; ok {
		files = append(files, filepath.Join(tmplDir, partial))
	}
	t, err := template.ParseFiles(files...)
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if data == nil {
		data = map[string]interface{}{}
	}
	data["CSRFToken"] = csrf.Token(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

func renderPartial(w http.ResponseWriter, r *http.Request, name string, data map[string]interface{}) {
	t, err := template.ParseFiles(filepath.Join(tmplDir, name))
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	t.Execute(w, data)
}

func renderError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.WriteHeader(status)
	renderTemplate(w, r, "error.html", map[string]interface{}{"Message": msg})
}

func requireSession(w http.ResponseWriter, r *http.Request) *session.Data {
	sess, err := session.Get(r)
	if err != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return nil
	}
	return sess
}

// Env var check used in templates
func getEnv(key string) string { return os.Getenv(key) }
