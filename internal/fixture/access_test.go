package fixture

import (
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrivateFixtureAccessProtectsIndexAndContentBeforeRegistration(t *testing.T) {
	server := NewServer()
	t.Cleanup(server.Close)
	token := rand.Text()
	if err := server.RequireAccessToken(token); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterHTML("/private-snapshot", "private snapshot body"); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNotFound || strings.Contains(recorder.Body.String(), "private") {
		t.Fatal("direct HTTP handler bypassed protected index admission")
	}
	for _, path := range []string{"/", "/private-snapshot"} {
		for _, candidate := range []string{"", rand.Text(), token} {
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL(path), nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set(AccessTokenHeader, candidate)
			response, err := server.HTTPServer().Client().Do(request)
			if err != nil {
				t.Fatal(err)
			}
			content, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil {
				t.Fatalf("actual protected response read/close: %v %v", readErr, closeErr)
			}
			if candidate == token {
				if response.StatusCode != http.StatusOK || !strings.Contains(string(content), "private") {
					t.Fatal("own token lost actual fixture access")
				}
			} else if response.StatusCode != http.StatusNotFound || strings.Contains(string(content), "private") {
				t.Fatal("foreign request discovered or read the private snapshot")
			}
		}
	}
	if err := server.RequireAccessToken(rand.Text()); err == nil {
		t.Fatal("live access owner changed after admission")
	}
}

func TestPrivateFixtureAccessRejectsInvalidLateAndClosedConfiguration(t *testing.T) {
	for _, token := range []string{"", "short", strings.Repeat("A", 129), strings.Repeat("A", 26) + "\r\n"} {
		server := NewServer()
		err := server.RequireAccessToken(token)
		server.Close()
		if err == nil {
			t.Fatal("invalid access token admitted")
		}
	}
	server := NewServer()
	t.Cleanup(server.Close)
	if err := server.RegisterHTML("/already-published", "actual fixture"); err != nil {
		t.Fatal(err)
	}
	if err := server.RequireAccessToken(rand.Text()); err == nil {
		t.Fatal("authentication configured after content publication")
	}
	closed := NewServer()
	closed.Close()
	if err := closed.RequireAccessToken(rand.Text()); err == nil {
		t.Fatal("closed server accepted an access owner")
	}
}
