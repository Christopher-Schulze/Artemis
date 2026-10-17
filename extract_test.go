package artemis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/scraper"
)

func extractServer(t *testing.T) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/list":
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "abc", Path: "/", HttpOnly: true})
			w.Header().Set("Content-Type", "text/html")
			next := ""
			if page < 3 {
				next = fmt.Sprintf(`<a class="next" href="/list?page=%d#top">next</a>`, page+1)
			}
			fmt.Fprintf(w, `<!doctype html><table><tr class="row"><td class="name">item-%d-a</td><td class="price">%d.50</td></tr><tr class="row"><td class="name">item-%d-b</td><td class="price">%d.75</td></tr></table>%s`, page, page, page, page, next)
		case "/cycle":
			w.Header().Set("Content-Type", "text/html")
			target := "/cycle?page=2"
			if r.URL.Query().Get("page") == "2" {
				target = "/cycle?page=1"
			}
			fmt.Fprintf(w, `<!doctype html><table><tr class="row"><td class="name">c</td><td class="price">1</td></tr></table><a class="next" href="%s">next</a>`, target)
		case "/external":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<!doctype html><table><tr class="row"><td class="name">e</td><td class="price">1</td></tr></table><a class="next" href="http://198.51.100.7/list">next</a>`)
		case "/api/items":
			if cookie, err := r.Cookie("sid"); err != nil || cookie.Value != "abc" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"token":"t-123","items":[{"name":"Lamp","price":"12.50"},{"name":"Desk","price":7}]}`)
		case "/api/big":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"items":[`+strings.Repeat(`{"name":"x","price":1},`, 220000)+`{"name":"y","price":2}]}`)
		case "/api/html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<p>not json</p>`)
		case "/api/redirect":
			http.Redirect(w, r, "http://198.51.100.7/api/items", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func startExtractSession(t *testing.T, server *httptest.Server, policy string) (*Session, context.Context) {
	t.Helper()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	engine, err := bridge.NewStaticNavigationPolicy([]byte(policy))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.Stop)
	agent, err := NewAgent(AgentConfig{PolicyEngine: engine, Chromium: &browserprocess.LaunchConfig{Headless: true}, PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := agent.Stop(); err != nil {
			t.Error(err)
		}
	})
	session, err := agent.CreateSession("extract")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	return session, ctx
}

var rowSchema = &scraper.StructuredSchema{Kind: scraper.KindList, Selector: ".row", Item: &scraper.StructuredSchema{Kind: scraper.KindObject, Selector: ".row", FieldsMap: map[string]*scraper.StructuredSchema{
	"name":  {Kind: scraper.KindText, Selector: ".name", IsRequired: true},
	"price": {Kind: scraper.KindNumber, Selector: ".price", IsRequired: true},
}}}

func TestGroundedExtractionAcrossChromiumPagesAndDynamicUpdates(t *testing.T) {
	server := extractServer(t)
	session, ctx := startExtractSession(t, server, `{"default":"allow"}`)
	pageID, _, taskErr := session.OpenChromiumPage(ctx, server.URL+"/list?page=1")
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	first, taskErr := session.ExtractGrounded(ctx, pageID, rowSchema)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	origin := strings.TrimSuffix(server.URL, "/")
	if !first.Complete || len(first.Value.([]any)) != 2 || first.Sources[0].URL != server.URL+"/list?page=1" || first.Sources[0].Trust.Origin != origin || first.Sources[0].Trust.Source != "extract" {
		t.Fatalf("grounded DOM extraction = %+v", first)
	}
	evaluate(t, ctx, session, pageID, `document.querySelector('.price').textContent='99.00'`)
	updated, taskErr := session.ExtractGrounded(ctx, pageID, rowSchema)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	if updated.Value.([]any)[0].(map[string]any)["price"] != 99.0 || updated.Sources[0].ContentHash == first.Sources[0].ContentHash || !updated.Sources[0].CapturedAt.After(first.Sources[0].CapturedAt) {
		t.Fatalf("dynamic update was not captured as a new source: %+v vs %+v", updated.Sources[0], first.Sources[0])
	}

	reload(t, session, pageID, server.URL+"/list?page=1")
	all, taskErr := session.ExtractGroundedPages(ctx, pageID, rowSchema, ExtractPagination{NextSelector: "a.next"})
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	if !all.Complete || all.StopReason != ExtractStopNoNextPage || len(all.Sources) != 3 || len(all.Value.([]any)) != 6 || all.Sources[2].URL != server.URL+"/list?page=3" {
		t.Fatalf("exhausted pagination = %+v", all)
	}
	if last := all.Fields[len(all.Fields)-1]; last.Source != 2 || last.Status != scraper.FieldFound {
		t.Fatalf("field not bound to its page source: %+v", last)
	}
	reload(t, session, pageID, server.URL+"/list?page=1")
	capped, taskErr := session.ExtractGroundedPages(ctx, pageID, rowSchema, ExtractPagination{NextSelector: "a.next", MaxPages: 2})
	if taskErr != nil || capped.Complete || capped.StopReason != ExtractStopMaxPages || len(capped.Sources) != 2 {
		t.Fatalf("capped pagination = %+v %v", capped, taskErr)
	}
	reload(t, session, pageID, server.URL+"/cycle?page=1")
	cycle, taskErr := session.ExtractGroundedPages(ctx, pageID, rowSchema, ExtractPagination{NextSelector: "a.next"})
	if taskErr != nil || cycle.StopReason != ExtractStopCycleClosed || len(cycle.Sources) != 2 || !cycle.Complete {
		t.Fatalf("cyclic pagination = %+v %v", cycle, taskErr)
	}
	reload(t, session, pageID, server.URL+"/external")
	left, taskErr := session.ExtractGroundedPages(ctx, pageID, rowSchema, ExtractPagination{NextSelector: "a.next"})
	if taskErr != nil || left.StopReason != ExtractStopLeftOrigin || left.Complete {
		t.Fatalf("cross-origin pagination = %+v %v", left, taskErr)
	}
	if _, taskErr := session.ExtractGroundedPages(ctx, pageID, rowSchema, ExtractPagination{NextSelector: "a.next", MaxPages: 500}); taskErr == nil || taskErr.Code != TaskErrorInvalidInput {
		t.Fatalf("unbounded pagination accepted: %v", taskErr)
	}
}

func TestGroundedExtractionFromRenderlessPages(t *testing.T) {
	server := extractServer(t)
	session, ctx := startExtractSession(t, server, `{"default":"allow"}`)
	pageID, _, taskErr := session.OpenPage(ctx, server.URL+"/list?page=1", false)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	all, taskErr := session.ExtractGroundedPages(ctx, pageID, rowSchema, ExtractPagination{NextSelector: "a.next"})
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	if !all.Complete || len(all.Sources) != 3 || len(all.Value.([]any)) != 6 || session.Page(pageID) == nil {
		t.Fatalf("renderless pagination = %+v", all)
	}
	if _, taskErr := session.ExtractResponseJSON(ctx, pageID, JSONResponseRequest{URL: "/api/items", Schema: scraper.JSONExtractSchema{Items: "/items", Fields: map[string]scraper.JSONExtractField{"name": {Pointer: "/name"}}}}); taskErr == nil || taskErr.Code != TaskErrorCapabilityUnavailable {
		t.Fatalf("renderless response extraction: %v", taskErr)
	}
}

func TestResponseExtractionStaysInsideTheSessionOrigin(t *testing.T) {
	server := extractServer(t)
	session, ctx := startExtractSession(t, server, `{"default":"allow"}`)
	pageID, _, taskErr := session.OpenChromiumPage(ctx, server.URL+"/list?page=1")
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	schema := scraper.JSONExtractSchema{Items: "/items", Fields: map[string]scraper.JSONExtractField{
		"name":  {Pointer: "/name", Kind: "string", Required: true},
		"price": {Pointer: "/price", Kind: "number", Required: true},
	}}
	result, taskErr := session.ExtractResponseJSON(ctx, pageID, JSONResponseRequest{URL: "/api/items", Schema: schema})
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Complete || len(result.Value.([]any)) != 2 || result.Sources[0].Kind != scraper.SourceJSONResponse || result.Sources[0].URL != server.URL+"/api/items" || strings.Contains(string(encoded), "t-123") || strings.Contains(string(encoded), "abc") {
		t.Fatalf("session-authorized JSON extraction = %s", encoded)
	}
	withheld, taskErr := session.ExtractResponseJSON(ctx, pageID, JSONResponseRequest{URL: "/api/items", Schema: scraper.JSONExtractSchema{Fields: map[string]scraper.JSONExtractField{"token": {Pointer: "/token"}}}})
	if taskErr != nil || withheld.Fields[0].Status != scraper.FieldWithheld {
		t.Fatalf("credential field = %+v %v", withheld, taskErr)
	}
	big, taskErr := session.ExtractResponseJSON(ctx, pageID, JSONResponseRequest{URL: "/api/big", Schema: schema})
	if taskErr != nil || !big.Sources[0].Truncated || big.Complete || big.Counts.Truncated == 0 {
		t.Fatalf("oversized response = %+v %v", big.Counts, taskErr)
	}
	for _, bad := range []struct {
		target string
		code   TaskErrorCode
	}{
		{"http://198.51.100.7/api/items", TaskErrorPolicyDenied},
		{"/api/redirect", TaskErrorExecutionFailed},
		{"/api/html", TaskErrorInvalidInput},
		{"/api/missing", TaskErrorExecutionFailed},
	} {
		if _, taskErr := session.ExtractResponseJSON(ctx, pageID, JSONResponseRequest{URL: bad.target, Schema: schema}); taskErr == nil || taskErr.Code != bad.code {
			t.Fatalf("%s: got %v, want %s", bad.target, taskErr, bad.code)
		}
	}

	denied, deniedCtx := startExtractSession(t, server, `{"default":"allow","deny":["extract"]}`)
	deniedPage, _, taskErr := denied.OpenChromiumPage(deniedCtx, server.URL+"/list?page=1")
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	if _, taskErr := denied.ExtractResponseJSON(deniedCtx, deniedPage, JSONResponseRequest{URL: "/api/items", Schema: schema}); taskErr == nil || taskErr.Code != TaskErrorPolicyDenied {
		t.Fatalf("policy-denied extraction ran: %v", taskErr)
	}
}
