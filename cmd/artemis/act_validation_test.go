package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestActRejectsInvalidScreenshotControlsBeforeConnection(t *testing.T) {
	var hits atomic.Int64
	peer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writer.WriteHeader(http.StatusBadRequest)
	}))
	defer peer.Close()
	endpoint := "ws" + strings.TrimPrefix(peer.URL, "http")
	for _, request := range []string{
		`{"kind":"screenshot","format":"gif"}`,
		`{"kind":"screenshot","format":"png","quality":80}`,
		`{"kind":"screenshot","format":"jpeg","quality":101}`,
		`{"kind":"screenshot","format":"png","forModel":true}`,
		`{"kind":"navigate","url":"https://example.test","forModel":true}`,
	} {
		t.Run(request, func(t *testing.T) {
			code := cmdAct([]string{"--server=" + endpoint, "--token=owned-test-token", "--client-id=client.test", "--session=session.test", "--page=page.test", "--timeout=50ms", "--request=" + request})
			if code != 2 || hits.Load() != 0 {
				t.Fatalf("invalid action passed admission: exit=%d peer hits=%d", code, hits.Load())
			}
		})
	}
}
