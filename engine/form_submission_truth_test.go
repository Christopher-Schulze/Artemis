package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
)

func TestEngineSubmitPreservesTextPlainRequest(t *testing.T) {
	var received atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Errorf("read body: %v", err)
			}
			if request.Header.Get("Content-Type") != agent.FormEncodingText || string(body) != "alpha=one\r\n" {
				t.Errorf("POST content-type=%q body=%q", request.Header.Get("Content-Type"), body)
			}
			received.Store(true)
		}
		_, _ = fmt.Fprint(w, `<html><body>ok</body></html>`)
	}))
	defer server.Close()
	engine, err := New(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	page, err := engine.Submit(context.Background(), agent.FormSubmission{
		URL: server.URL, Method: http.MethodPost, ContentType: agent.FormEncodingText, Body: []byte("alpha=one\r\n"),
	}, FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
	if !received.Load() {
		t.Fatal("text/plain submission did not reach server")
	}
}

func TestFileFormEscalatesBeforeEngineRequest(t *testing.T) {
	var postCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			postCount.Add(1)
		}
		_, _ = fmt.Fprint(w, `<html><body><form id="upload" method="post" enctype="multipart/form-data"><input type="file" name="payload"></form></body></html>`)
	}))
	defer server.Close()
	engine, err := New(testConfig(server))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	page, err := engine.Fetch(context.Background(), server.URL, FetchOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
	form := agent.FindForm(page.Document(), "#upload")
	if form == nil {
		t.Fatal("upload form missing")
	}
	submission, err := form.Submit()
	if !errors.Is(err, agent.ErrFormSubmissionRequiresBrowser) || submission.URL != "" || submission.Method != "" || submission.ContentType != "" || len(submission.Body) != 0 {
		t.Fatalf("file submission=%+v err=%v", submission, err)
	}
	if postCount.Load() != 0 {
		t.Fatalf("unsupported file form emitted %d POST requests", postCount.Load())
	}
	result, err := page.FormSubmit(context.Background(), "#upload")
	if !errors.Is(err, agent.ErrFormSubmissionRequiresBrowser) || result.Success {
		t.Fatalf("renderless submit result=%+v err=%v", result, err)
	}
	var unsupported *agent.FormSubmissionUnsupportedError
	if !errors.As(err, &unsupported) || unsupported.EncType != agent.FormEncodingMultipart {
		t.Fatalf("renderless submit error=%+v", unsupported)
	}
	results, err := page.Form(context.Background(), "#upload", map[string]string{`input[name="payload"]`: "opaque"}, true)
	if !errors.Is(err, agent.ErrFormSubmissionRequiresBrowser) || len(results) != 1 || results[0].Type != actions.FormActionSubmit || results[0].Success {
		t.Fatalf("renderless form results=%+v err=%v", results, err)
	}
	unsupported = nil
	if !errors.As(err, &unsupported) || unsupported.EncType != agent.FormEncodingMultipart {
		t.Fatalf("renderless form error=%+v", unsupported)
	}
	fileInput, queryErr := page.Document().QuerySelector(`input[name="payload"]`)
	if queryErr != nil || fileInput == nil || fileInput.AttrOrEmpty("value") != "" {
		t.Fatalf("unsupported form mutated file input: node=%v err=%v", fileInput, queryErr)
	}
}
