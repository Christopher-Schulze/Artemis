package engine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActualHTTPUnhandledPromiseRejectsPageAdmission(t *testing.T) {
	for _, source := range []string{
		`Promise.reject(new Error('actual-unhandled-http'))`,
		`document.addEventListener('DOMContentLoaded',async()=>{throw Error('actual-unhandled-http')})`,
	} {
		t.Run(source, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeTestBody(t, w, `<body><script>`+source+`</script></body>`)
			}))
			defer server.Close()
			owner, err := New(testConfig(server))
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestResource(t, "engine", owner.Close)
			page, err := owner.Fetch(t.Context(), server.URL, FetchOpts{RunScripts: true})
			if page != nil {
				closeTestResource(t, "unexpected page", page.Close)
			}
			if page != nil || err == nil || !strings.Contains(err.Error(), "actual-unhandled-http") {
				t.Fatalf("actual rejection acknowledged page admission: page=%v err=%v", page != nil, err)
			}
		})
	}
}
