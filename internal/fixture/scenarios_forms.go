package fixture

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
)

func formScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "form-001",
			Path:        "/form-001",
			Kind:        KindForm,
			Description: "POST form with text and email fields",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Form Fixture</title>
</head><body>
<form id="contact" action="/form-submit" method="post">
<label>Name: <input type="text" name="name" value=""></label>
<label>Email: <input type="email" name="email" value=""></label>
<button type="submit">Send</button>
</form>
</body></html>`,
			Expect: Expect{
				Status:   200,
				Title:    "Form Fixture",
				Contains: []string{"Name:", "Email:", "Send"},
			},
		},
		{
			ID:          "form-002",
			Path:        "/form-002",
			Kind:        KindForm,
			Description: "GET search form",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Search Form</title>
</head><body>
<form id="search" action="/form-search" method="get">
<label>Search: <input type="search" name="q" value="" placeholder="Search..."></label>
<button type="submit">Search</button>
</form>
</body></html>`,
			Expect: Expect{
				Status:   200,
				Title:    "Search Form",
				Contains: []string{"Search:", "Search"},
			},
		},
		{
			ID:          "form-submit",
			Path:        "/form-submit",
			Kind:        KindForm,
			Description: "POST handler for form-001",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					http.Error(w, "POST required", http.StatusMethodNotAllowed)
					return
				}
				if err := r.ParseForm(); err != nil {
					http.Error(w, "bad form", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, "<!doctype html><html><head><title>Submitted</title></head><body>Submitted name=%s email=%s</body></html>",
					html.EscapeString(r.FormValue("name")),
					html.EscapeString(r.FormValue("email")))
			}),
			Expect: Expect{
				Status:   405,
				Contains: []string{"POST required"},
			},
		},
		{
			ID:          "form-search",
			Path:        "/form-search",
			Kind:        KindForm,
			Description: "GET handler for form-002",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					http.Error(w, "GET required", http.StatusMethodNotAllowed)
					return
				}
				q := r.URL.Query().Get("q")
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, "<!doctype html><html><head><title>Search Result</title></head><body>Search: %s</body></html>",
					html.EscapeString(q))
			}),
			Expect: Expect{
				Status:   200,
				Contains: []string{"Search:"},
			},
		},
	}
}

func fileScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "file-001",
			Path:        "/file-001",
			Kind:        KindFile,
			Description: "Multipart file upload form",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>File Upload</title>
</head><body>
<form id="upload" action="/file-upload" method="post" enctype="multipart/form-data">
<label>File: <input type="file" name="file"></label>
<button type="submit">Upload</button>
</form>
</body></html>`,
			Expect: Expect{
				Status:   200,
				Title:    "File Upload",
				Contains: []string{"File:", "Upload"},
			},
		},
		{
			ID:          "file-upload",
			Path:        "/file-upload",
			Kind:        KindFile,
			Description: "Handler for file-001 uploads",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					http.Error(w, "POST required", http.StatusMethodNotAllowed)
					return
				}
				if err := r.ParseMultipartForm(32 << 20); err != nil {
					http.Error(w, "bad multipart", http.StatusBadRequest)
					return
				}
				file, _, err := r.FormFile("file")
				if err != nil {
					http.Error(w, "missing file", http.StatusBadRequest)
					return
				}
				defer file.Close()
				data, err := io.ReadAll(file)
				if err != nil {
					http.Error(w, "read error", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				fmt.Fprintf(w, "<!doctype html><html><head><title>Uploaded</title></head><body>Received file size: %d contents: %s</body></html>",
					len(data), html.EscapeString(strings.TrimSpace(string(data))))
			}),
			Expect: Expect{
				Status:   405,
				Contains: []string{"POST required"},
			},
		},
	}
}
