package frontend

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// renderer handles template rendering.
type renderer struct {
	baseTemplate *template.Template // Base template with nav, components
	templatesFS  fs.FS              // Embedded filesystem for page templates
	config       *Config
	funcs        template.FuncMap
}

// newRenderer creates a new renderer.
func newRenderer(baseTemplate *template.Template, templatesFS fs.FS, cfg *Config) *renderer {
	return &renderer{
		baseTemplate: baseTemplate,
		templatesFS:  templatesFS,
		config:       cfg,
		funcs:        templateFuncs(),
	}
}

// PageData contains common data for all pages.
type PageData struct {
	Title           string
	BasePath        string
	CurrentPath     string
	TenantID        string
	ReadOnly        bool
	RefreshInterval int // in seconds
	Flash           *FlashMessage
	Data            any
}

// FlashMessage represents a flash message.
type FlashMessage struct {
	Type    string // "success", "error", "warning", "info"
	Message string
}

// render renders a template with the given data.
// It clones the base template and parses the page-specific template into it,
// avoiding conflicts between "content" blocks in different pages.
func (r *renderer) render(w http.ResponseWriter, req *http.Request, name string, data any) error {
	pageData := PageData{
		BasePath:        r.config.BasePath,
		CurrentPath:     req.URL.Path,
		TenantID:        r.config.TenantID,
		ReadOnly:        r.config.ReadOnly,
		RefreshInterval: int(r.config.RefreshInterval.Seconds()),
		Data:            data,
	}

	// Clone the base template to avoid conflicts between page "content" blocks
	tmpl, err := r.baseTemplate.Clone()
	if err != nil {
		return fmt.Errorf("clone template: %w", err)
	}

	// Parse the page-specific template into the clone
	pageTemplatePath := "templates/" + name
	_, err = tmpl.ParseFS(r.templatesFS, pageTemplatePath)
	if err != nil {
		return fmt.Errorf("parse page template %s: %w", pageTemplatePath, err)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return tmpl.ExecuteTemplate(w, "base", pageData)
}

// renderFragment renders a template fragment (no layout).
// Fragment templates define their template name as the file path (e.g., "chat/pending.html").
func (r *renderer) renderFragment(w http.ResponseWriter, name string, data any) error {
	// Clone the base template
	tmpl, err := r.baseTemplate.Clone()
	if err != nil {
		return fmt.Errorf("clone template: %w", err)
	}

	// Parse the fragment template
	fragmentTemplatePath := "templates/" + name
	_, err = tmpl.ParseFS(r.templatesFS, fragmentTemplatePath)
	if err != nil {
		return fmt.Errorf("parse fragment template %s: %w", fragmentTemplatePath, err)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Fragment templates define their template name as the file path (e.g., "chat/pending.html")
	return tmpl.ExecuteTemplate(w, name, data)
}

// Template helper functions

func formatDuration(d *time.Duration) string {
	if d == nil {
		return "-"
	}
	if *d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if *d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if *d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04:05")
}

func formatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day ago"
	}
	return fmt.Sprintf("%d days ago", days)
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1000000)
}

func truncate(n int, v any) string {
	var s string
	switch val := v.(type) {
	case string:
		s = val
	case fmt.Stringer:
		s = val.String()
	default:
		s = fmt.Sprintf("%v", v)
	}
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

func stateColor(state string) string {
	switch state {
	case "pending":
		return "text-yellow-600"
	case "batch_submitting", "batch_pending", "batch_processing", "streaming":
		return "text-blue-600"
	case "pending_tools":
		return "text-purple-600"
	case "completed":
		return "text-green-600"
	case "failed":
		return "text-red-600"
	case "cancelled":
		return "text-gray-600"
	case "running":
		return "text-blue-600"
	default:
		return "text-gray-600"
	}
}

func stateBgColor(state string) string {
	switch state {
	case "pending":
		return "bg-yellow-100 text-yellow-800"
	case "batch_submitting", "batch_pending", "batch_processing", "streaming":
		return "bg-blue-100 text-blue-800"
	case "pending_tools":
		return "bg-purple-100 text-purple-800"
	case "completed":
		return "bg-green-100 text-green-800"
	case "failed":
		return "bg-red-100 text-red-800"
	case "cancelled":
		return "bg-gray-100 text-gray-800"
	case "running":
		return "bg-blue-100 text-blue-800"
	default:
		return "bg-gray-100 text-gray-800"
	}
}

func jsonEncode(v any) string {
	// Handle []byte specially - it's already JSON, so parse and re-indent it
	// instead of base64 encoding it
	if b, ok := v.([]byte); ok {
		if len(b) == 0 {
			return "{}"
		}
		var parsed any
		if err := json.Unmarshal(b, &parsed); err != nil {
			// If it's not valid JSON, return as string
			return string(b)
		}
		indented, err := json.MarshalIndent(parsed, "", "  ")
		if err != nil {
			return string(b)
		}
		return string(indented)
	}

	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	return string(b)
}

func safeHTML(s string) template.HTML {
	return template.HTML(s)
}

func add(a, b int) int {
	return a + b
}

func sub(a, b int) int {
	return a - b
}

func mul(a, b int) int {
	return a * b
}

func mulFloat(a float64, b int) float64 {
	return a * float64(b)
}

func div(a, b int) int {
	if b == 0 {
		return 0
	}
	return a / b
}

func seq(start, end int) []int {
	if start > end {
		return nil
	}
	result := make([]int, end-start+1)
	for i := range result {
		result[i] = start + i
	}
	return result
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func defaultVal(val, def any) any {
	if val == nil {
		return def
	}
	switch v := val.(type) {
	case string:
		if v == "" {
			return def
		}
	case int:
		if v == 0 {
			return def
		}
	}
	return val
}
