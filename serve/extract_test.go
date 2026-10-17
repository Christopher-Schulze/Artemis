package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/scraper"
)

func TestPageExtractCommandReturnsGroundedLabeledResults(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestResponse(t, w, `<!doctype html><div class="p"><span class="n">Lamp</span><span class="v">1</span><span class="v">2</span></div>`)
	}))
	defer page.Close()
	agent := testChromiumAgent(t)
	defer stopTestAgent(t, agent)
	session, err := agent.CreateSession("extract")
	if err != nil {
		t.Fatal(err)
	}
	pageID, _, taskErr := session.OpenPage(t.Context(), page.URL, false)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	server := New(agent, Opts{AuthToken: testAuthToken})
	client := clientIdentity{id: "client.test", ownerRef: "test", rateKey: "127.0.0.1"}
	server.trackSession(session.SessionID(), client.id)
	schema := &scraper.StructuredSchema{Kind: scraper.KindObject, Selector: ".p", FieldsMap: map[string]*scraper.StructuredSchema{
		"name":  {Kind: scraper.KindText, Selector: ".n", IsRequired: true},
		"value": {Kind: scraper.KindNumber, Selector: ".v", IsRequired: true},
	}}
	resp := dispatchTyped(t, server, client, "1", CmdPageExtract, PageExtractParams{SessionID: session.SessionID(), PageID: pageID, Schema: schema})
	var result scraper.GroundedExtraction
	mustDecode(t, *resp, &result)
	assertTrust(t, "page.extract", resp.Trust, strings.TrimSuffix(page.URL, "/"), "page.extract", false)
	if result.Complete || result.Counts.Found != 1 || result.Counts.Conflict != 1 || len(result.Sources) != 1 || result.Sources[0].Trust.Origin != strings.TrimSuffix(page.URL, "/") {
		t.Fatalf("grounded result = %+v", result)
	}
	for _, bad := range []PageExtractParams{
		{SessionID: session.SessionID(), PageID: pageID},
		{SessionID: session.SessionID(), PageID: pageID, Schema: schema, JSON: &artemis.JSONResponseRequest{URL: "/x"}},
	} {
		if resp := dispatchTyped(t, server, client, "bad", CmdPageExtract, bad); resp.OK || resp.Error.Code != string(ErrBadParams) {
			t.Fatalf("ambiguous extract request accepted: %+v", resp)
		}
	}
	if resp := dispatchTyped(t, server, client, "2", CmdPageExtract, PageExtractParams{SessionID: session.SessionID(), PageID: pageID, JSON: &artemis.JSONResponseRequest{URL: "/api", Schema: scraper.JSONExtractSchema{Fields: map[string]scraper.JSONExtractField{"a": {Pointer: "/a"}}}}}); resp.OK || resp.Error.Code != string(ErrCapabilityUnavailable) {
		t.Fatalf("renderless JSON extraction: %+v", resp)
	}
}
