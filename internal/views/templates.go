package views

import (
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"time"
)

type Renderer struct {
	templates *template.Template
}

func NewRenderer(glob string) (*Renderer, error) {
	funcMap := template.FuncMap{
		"fmtTime": func(value *time.Time, loc *time.Location) string {
			if value == nil {
				return ""
			}
			if loc == nil {
				loc = time.UTC
			}
			return value.In(loc).Format("2006-01-02 15:04")
		},
		"fmtDate": func(value time.Time, loc *time.Location) string {
			if loc == nil {
				loc = time.UTC
			}
			return value.In(loc).Format("2006-01-02")
		},
		"fmtDateTimeLocal": func(value *time.Time, loc *time.Location) string {
			if value == nil {
				return ""
			}
			if loc == nil {
				loc = time.UTC
			}
			return value.In(loc).Format("2006-01-02T15:04")
		},
	}

	parsed, err := template.New(filepath.Base(glob)).Funcs(funcMap).ParseGlob(glob)
	if err != nil {
		return nil, fmt.Errorf("parse templates from %s: %w", glob, err)
	}
	return &Renderer{templates: parsed}, nil
}

func (r *Renderer) Render(w http.ResponseWriter, name string, data any) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := r.templates.ExecuteTemplate(w, name, data); err != nil {
		return fmt.Errorf("execute template %s: %w", name, err)
	}
	return nil
}
