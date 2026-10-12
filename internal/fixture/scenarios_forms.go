package fixture

import (
	"errors"
	"fmt"
	"html"
	"html/template"
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
				if _, err := fmt.Fprintf(w, "<!doctype html><html><head><title>Submitted</title></head><body>Submitted name=%s email=%s</body></html>",
					html.EscapeString(r.FormValue("name")),
					html.EscapeString(r.FormValue("email"))); err != nil {
					return
				}
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
				if _, err := fmt.Fprintf(w, "<!doctype html><html><head><title>Search Result</title></head><body>Search: %s</body></html>",
					html.EscapeString(q)); err != nil {
					return
				}
			}),
			Expect: Expect{
				Status:   200,
				Contains: []string{"Search:"},
			},
		},
	}
}

const fixtureUploadMaxBytes int64 = 32 << 20

func readFixtureUpload(w http.ResponseWriter, r *http.Request, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, errors.New("multipart upload: positive size limit required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, fmt.Errorf("multipart upload: reader: %w", err)
	}

	var fileData []byte
	found := false
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, fmt.Errorf("multipart upload: next part: %w", nextErr)
		}
		if part.FormName() != "file" || found {
			if closeErr := part.Close(); closeErr != nil {
				return nil, fmt.Errorf("multipart upload: close ignored part: %w", closeErr)
			}
			continue
		}

		found = true
		fileData, err = io.ReadAll(io.LimitReader(part, maxBytes+1))
		closeErr := part.Close()
		if err != nil {
			return nil, fmt.Errorf("multipart upload: read file: %w", err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("multipart upload: close file: %w", closeErr)
		}
		if int64(len(fileData)) > maxBytes {
			return nil, fmt.Errorf("multipart upload: file exceeds %d bytes", maxBytes)
		}
	}
	if !found {
		return nil, errors.New("multipart upload: file field required")
	}
	return fileData, nil
}

func fileScenarios() []Scenario {
	const uploadResponse = `<!doctype html><html><head><title>Uploaded</title></head><body>Received file size: {{.Size}} contents: {{.Contents}}</body></html>`
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
				data, err := readFixtureUpload(w, r, fixtureUploadMaxBytes)
				if err != nil {
					http.Error(w, "bad multipart", http.StatusBadRequest)
					return
				}
				uploadTemplate, templateErr := template.New("file-upload").Parse(uploadResponse)
				if templateErr != nil {
					http.Error(w, "upload response unavailable", http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				if err := uploadTemplate.Execute(w, struct {
					Size     int
					Contents string
				}{Size: len(data), Contents: strings.TrimSpace(string(data))}); err != nil {
					return
				}
			}),
			Expect: Expect{
				Status:   405,
				Contains: []string{"POST required"},
			},
		},
	}
}
