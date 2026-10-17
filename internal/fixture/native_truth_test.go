package fixture

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCrossResultRejectsUnknownStatusAndEverySemanticFailure(t *testing.T) {
	scenario := Scenario{Expect: Expect{Status: 404, Title: "Absent", Contains: []string{"actual"}, NotContains: []string{"forbidden"}, URL: "/missing", Eval: "value", EvalContains: "observed"}}
	valid := CrossResult{StatusCode: 404, Title: "Absent", Text: "actual", URL: "https://fixture.test/missing", EvalResult: "observed"}
	if err := ValidateCrossResult(scenario, valid); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*CrossResult)
	}{
		{"unknown status", func(value *CrossResult) { value.StatusCode = 0 }},
		{"fabricated 200", func(value *CrossResult) { value.StatusCode = 200 }},
		{"wrong title", func(value *CrossResult) { value.Title = "Other" }},
		{"missing text", func(value *CrossResult) { value.Text = "other" }},
		{"forbidden text", func(value *CrossResult) { value.Text = "actual forbidden" }},
		{"wrong URL", func(value *CrossResult) { value.URL = "https://fixture.test/other" }},
		{"wrong eval", func(value *CrossResult) { value.EvalResult = "other" }},
		{"eval failed", func(value *CrossResult) { value.EvalErr = "actual failure" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value := valid
			tc.change(&value)
			if err := ValidateCrossResult(scenario, value); err == nil {
				t.Fatal("incomplete/wrong actual result passed")
			}
		})
	}
}

func TestNativeFixtureResponseStatusUsesActualDocumentNotPageOrChild(t *testing.T) {
	server := NewServer()
	defer server.Close()
	body := "<html><head><title>Absent</title></head><body>actual document<iframe src='/child'></iframe><script>window.status = '200';</script></body></html>"
	if err := server.RegisterRaw("/missing", "text/html", 404, []byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterRaw("/child", "text/html", 200, []byte("<html><body>child</body></html>")); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterHandler("/redirect", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/missing", http.StatusFound) })); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	for _, runner := range []runner{&bridgeRunner{}, &artemisRunner{}} {
		t.Run(runner.name(), func(t *testing.T) {
			defer func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				runner.stop(cleanup, t)
			}()
			runner.start(ctx, t, server)
			for _, route := range []string{"/missing", "/redirect"} {
				t.Run(route, func(t *testing.T) {
					scenario := Scenario{ID: "observed", Path: route, Kind: KindHTML, Expect: Expect{Status: 404, Title: "Absent", Contains: []string{"actual document"}, URL: "/missing"}}
					actual := runner.run(ctx, t, scenario, server)
					if err := ValidateCrossResult(scenario, actual); err != nil {
						t.Fatalf("actual native response: %v %+v", err, actual)
					}
					if !strings.HasSuffix(actual.URL, "/missing") {
						t.Fatalf("redirect/child response substituted actual URL: %q", actual.URL)
					}
				})
			}
		})
	}
}
