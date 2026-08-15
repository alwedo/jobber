package server

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"text/template"
	"time"

	"github.com/alwedo/jobber/db"
)

type htmlRenderer struct {
	templateFS      fs.FS
	sharedTemplates *template.Template
}

// The newHTMLRenderer function creates a new htmlRenderer containing a shared
// set of parsed templates with support for any custom template functions.
func newHTMLRenderer(templateFS fs.FS, sharedTemplateFiles ...string) (*htmlRenderer, error) {
	funcs := template.FuncMap{
		"pubDate": func(o *db.Offer) string {
			return o.PostedAt.Time.Format(time.RFC1123Z)
		},
		"postedAt": func(o *db.Offer) string {
			return o.PostedAt.Time.Format("Jan 2")
		},
	}

	sharedTemplates, err := template.New("").Funcs(funcs).ParseFS(templateFS, sharedTemplateFiles...)
	if err != nil {
		return nil, err
	}

	r := &htmlRenderer{
		templateFS:      templateFS,
		sharedTemplates: sharedTemplates,
	}

	return r, nil
}

// The render method clones the shared template set, optionally parses additional
// templates, executes the named template with the supplied data, and writes the
// response.
func (h *htmlRenderer) render(w http.ResponseWriter, data any, templateName string, additionalTemplateFiles ...string) error {
	ts, err := h.sharedTemplates.Clone()
	if err != nil {
		return fmt.Errorf("cloning shared templates in render: %w", err)
	}

	if len(additionalTemplateFiles) > 0 {
		ts, err = ts.ParseFS(h.templateFS, additionalTemplateFiles...)
		if err != nil {
			return fmt.Errorf("parseFS in render: %w", err)
		}
	}

	buf := new(bytes.Buffer)
	err = ts.ExecuteTemplate(buf, templateName, data)
	if err != nil {
		return fmt.Errorf("executing template in render: %w", err)
	}

	w.WriteHeader(http.StatusOK)
	if _, err := buf.WriteTo(w); err != nil {
		return fmt.Errorf("buf.WriteTo in render: %w", err)
	}

	return nil
}
