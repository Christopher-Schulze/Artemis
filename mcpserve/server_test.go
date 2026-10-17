package mcpserve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/Christopher-Schulze/Artemis/security"
)

const fixturePage = `<!doctype html><title>Fixture</title><body>
<button onclick="document.getElementById('out').textContent='pressed'">Press</button><p id="out">idle</p>
<form toolname="search_flights" tooldescription="Search flights" toolautosubmit
 onsubmit="event.preventDefault();if(event.respondWith)event.respondWith(Promise.resolve('ok '+this.from.value))"><input name="from" required></form>
<form toolname="slow_tool" tooldescription="Waits for a human submit" onsubmit="event.preventDefault()"><input name="q"><button type="submit">Go</button></form>
</body>`

type fixture struct {
	client *mcp.ClientSession
	server *Server
	url    string
}

func startFixture(t *testing.T) fixture {
	t.Helper()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, fixturePage)
	}))
	t.Cleanup(page.Close)
	parsed, err := url.Parse(page.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	policy, err := bridge.NewStaticNavigationPolicy([]byte(`{"default":"allow"}`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(policy.Stop)
	agent, err := artemis.NewAgent(artemis.AgentConfig{PolicyEngine: policy, Chromium: &browserprocess.LaunchConfig{Headless: true, ExtraArgs: []string{"--enable-experimental-web-platform-features"}}, PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := agent.Stop(); err != nil {
			t.Error(err)
		}
	})
	server := New(agent, "test")
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.MCP().Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
	})
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
		if err := serverSession.Wait(); err != nil && !strings.Contains(err.Error(), "closed") {
			t.Error(err)
		}
	})
	return fixture{client: session, server: server, url: page.URL}
}

type envelope struct {
	Trust *security.ContentTrust `json:"trust"`
	Value json.RawMessage        `json:"value"`
}

func call(t *testing.T, ctx context.Context, f fixture, name string, arguments any) (envelope, string) {
	t.Helper()
	result, err := f.client.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if result.IsError {
		return envelope{}, text
	}
	var decoded envelope
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("%s: %v in %s", name, err, text)
	}
	return decoded, ""
}

func TestMCPToolsDriveTheSessionOwnerWithTrustAndTypedErrors(t *testing.T) {
	f := startFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if init := f.client.InitializeResult(); init == nil || init.ServerInfo.Name != "artemis" {
		t.Fatalf("initialize result = %+v", init)
	}
	tools, err := f.client.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tool := range tools.Tools {
		names[tool.Name] = tool.InputSchema != nil
	}
	for _, want := range []string{"artemis_open", "artemis_dump", "artemis_observe", "artemis_act", "artemis_query", "artemis_visual", "artemis_visual_act", "artemis_extract", "artemis_close", "artemis_webmcp_tools", "artemis_webmcp_call"} {
		if !names[want] {
			t.Fatalf("tool %s missing or without input schema: %v", want, names)
		}
	}

	opened, failed := call(t, ctx, f, "artemis_open", map[string]any{"url": f.url})
	if failed != "" {
		t.Fatal(failed)
	}
	var renderless struct {
		PageID string `json:"pageId"`
		Engine string `json:"engine"`
	}
	if err := json.Unmarshal(opened.Value, &renderless); err != nil || renderless.Engine != "renderless" || opened.Trust == nil || opened.Trust.Trust != security.TrustUntrusted {
		t.Fatalf("renderless open = %s %+v %v", opened.Value, opened.Trust, err)
	}
	dumped, failed := call(t, ctx, f, "artemis_dump", map[string]any{"pageId": renderless.PageID, "format": "title"})
	if failed != "" || !strings.Contains(string(dumped.Value), "Fixture") || dumped.Trust.Source != "artemis_dump" {
		t.Fatalf("dump = %s %+v %s", dumped.Value, dumped.Trust, failed)
	}

	extracted, failed := call(t, ctx, f, "artemis_extract", map[string]any{"pageId": renderless.PageID, "schema": map[string]any{"kind": "object", "selector": "body", "fields": map[string]any{"out": map[string]any{"kind": "text", "selector": "#out", "required": true}}}})
	if failed != "" || !strings.Contains(string(extracted.Value), `"complete":true`) || !strings.Contains(string(extracted.Value), `"nodePath"`) {
		t.Fatalf("extract = %s %s", extracted.Value, failed)
	}
	if _, failed := call(t, ctx, f, "artemis_extract", map[string]any{"pageId": renderless.PageID}); !strings.HasPrefix(failed, "invalid_input:") {
		t.Fatalf("extract without schema accepted: %q", failed)
	}

	opened, failed = call(t, ctx, f, "artemis_open", map[string]any{"url": f.url, "chromium": true})
	if failed != "" {
		t.Fatal(failed)
	}
	var chromium struct {
		PageID string `json:"pageId"`
	}
	if err := json.Unmarshal(opened.Value, &chromium); err != nil || chromium.PageID == "" {
		t.Fatalf("chromium open = %s %v", opened.Value, err)
	}
	observed, failed := call(t, ctx, f, "artemis_observe", map[string]any{"pageId": chromium.PageID})
	if failed != "" {
		t.Fatal(failed)
	}
	var evidence struct {
		Snapshot struct {
			Epoch uint64 `json:"epoch"`
			Nodes []struct {
				Ref  string `json:"ref"`
				Role string `json:"role"`
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(observed.Value, &evidence); err != nil {
		t.Fatal(err)
	}
	press := ""
	for _, node := range evidence.Snapshot.Nodes {
		if node.Role == "button" && node.Name == "Press" {
			press = node.Ref
		}
	}
	if press == "" || evidence.Snapshot.Epoch == 0 {
		t.Fatalf("observation lacks the button ref: %s", observed.Value)
	}
	if _, failed := call(t, ctx, f, "artemis_act", map[string]any{"pageId": chromium.PageID, "snapshotEpoch": evidence.Snapshot.Epoch + 1, "request": map[string]any{"kind": "click", "ref": press}}); !strings.HasPrefix(failed, "stale_reference:") {
		t.Fatalf("stale epoch was not refused with a typed error: %q", failed)
	}
	if _, failed := call(t, ctx, f, "artemis_act", map[string]any{"pageId": chromium.PageID, "request": map[string]any{"kind": "click", "ref": press, "smuggled": true}}); !strings.HasPrefix(failed, "invalid_input:") {
		t.Fatalf("unknown request field accepted: %q", failed)
	}
	acted, failed := call(t, ctx, f, "artemis_act", map[string]any{"pageId": chromium.PageID, "snapshotEpoch": evidence.Snapshot.Epoch, "request": map[string]any{"kind": "click", "ref": press}})
	if failed != "" || !strings.Contains(string(acted.Value), `"success":true`) {
		t.Fatalf("act = %s %s", acted.Value, failed)
	}

	queried, failed := call(t, ctx, f, "artemis_query", map[string]any{"pageId": chromium.PageID, "request": map[string]any{"options": map[string]any{}}})
	if failed != "" || !strings.Contains(string(queried.Value), `"revision"`) {
		t.Fatalf("query = %s %s", queried.Value, failed)
	}
	visual, err := f.client.CallTool(ctx, &mcp.CallToolParams{Name: "artemis_visual", Arguments: map[string]any{"pageId": chromium.PageID, "annotate": true}})
	if err != nil || visual.IsError || len(visual.Content) != 2 {
		t.Fatalf("visual = %+v %v", visual, err)
	}
	image, ok := visual.Content[1].(*mcp.ImageContent)
	if !ok || image.MIMEType != "image/png" || len(image.Data) < 8 || string(image.Data[1:4]) != "PNG" {
		t.Fatalf("visual image content = %+v", visual.Content[1])
	}
	var observation artemis.VisualObservation
	var visualEnvelope envelope
	if err := json.Unmarshal([]byte(visual.Content[0].(*mcp.TextContent).Text), &visualEnvelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(visualEnvelope.Value, &observation); err != nil || observation.ID == "" || len(observation.Image) != 0 {
		t.Fatalf("visual observation = %s %v", visualEnvelope.Value, err)
	}
	hovered := false
	for _, annotation := range observation.Annotations {
		if annotation.Name == "Press" {
			hovered = true
			box := annotation.Box
			if _, failed := call(t, ctx, f, "artemis_visual_act", map[string]any{"pageId": chromium.PageID, "observationId": observation.ID, "kind": "hover", "x": box.X + box.Width/2, "y": box.Y + box.Height/2}); failed != "" {
				t.Fatalf("visual act = %s", failed)
			}
		}
	}
	if !hovered {
		t.Fatalf("visual observation lacks the Press reference: %+v", observation.Annotations)
	}

	resources, err := f.client.ListResources(ctx, nil)
	if err != nil || len(resources.Resources) != 1 || resources.Resources[0].URI != observationURI(chromium.PageID) {
		t.Fatalf("page observation resource not listed: %+v %v", resources, err)
	}
	read, err := f.client.ReadResource(ctx, &mcp.ReadResourceParams{URI: observationURI(chromium.PageID)})
	if err != nil || len(read.Contents) != 1 || !strings.Contains(read.Contents[0].Text, `"trust"`) || !strings.Contains(read.Contents[0].Text, "pressed") {
		t.Fatalf("observation resource = %+v %v", read, err)
	}
	if _, err := f.client.ReadResource(ctx, &mcp.ReadResourceParams{URI: observationURI("missing")}); err == nil {
		t.Fatal("observation of an unknown page was served")
	}
	if _, failed := call(t, ctx, f, "artemis_close", map[string]any{"pageId": chromium.PageID}); failed != "" {
		t.Fatal(failed)
	}
	if resources, err := f.client.ListResources(ctx, nil); err != nil || len(resources.Resources) != 0 {
		t.Fatalf("closed page resource still listed: %+v %v", resources, err)
	}
}

func TestMCPWebMCPCallsAndCancellationReachTheBrowser(t *testing.T) {
	f := startFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	opened, failed := call(t, ctx, f, "artemis_open", map[string]any{"url": f.url, "chromium": true})
	if failed != "" {
		t.Fatal(failed)
	}
	var page struct {
		PageID string `json:"pageId"`
	}
	if err := json.Unmarshal(opened.Value, &page); err != nil {
		t.Fatal(err)
	}
	listed, failed := call(t, ctx, f, "artemis_webmcp_tools", map[string]any{"pageId": page.PageID})
	if failed != "" {
		t.Fatal(failed)
	}
	var catalog artemis.WebMCPCatalog
	if err := json.Unmarshal(listed.Value, &catalog); err != nil || len(catalog.Tools) != 2 {
		t.Fatalf("catalog = %s %v", listed.Value, err)
	}
	frameID := catalog.Tools[0].FrameID
	result, failed := call(t, ctx, f, "artemis_webmcp_call", map[string]any{"pageId": page.PageID, "revision": catalog.Revision, "frameId": frameID, "toolName": "search_flights", "input": map[string]any{"from": "BER"}})
	if failed != "" || !strings.Contains(string(result.Value), `"output":"ok BER"`) || result.Trust == nil || result.Trust.Source != "webmcp" {
		t.Fatalf("webmcp call = %s %+v %s", result.Value, result.Trust, failed)
	}
	if _, failed := call(t, ctx, f, "artemis_webmcp_call", map[string]any{"pageId": page.PageID, "revision": catalog.Revision, "frameId": frameID, "toolName": "search_flights", "input": map[string]any{"from": 7}}); !strings.HasPrefix(failed, "invalid_input:") {
		t.Fatalf("schema-invalid input dispatched: %q", failed)
	}

	// A cancelled MCP request cancels the browser invocation: the page lease
	// is released at once instead of after the 30s invocation bound.
	cancelled, stop := context.WithCancel(ctx)
	timer := time.AfterFunc(500*time.Millisecond, stop)
	defer timer.Stop()
	if _, err := f.client.CallTool(cancelled, &mcp.CallToolParams{Name: "artemis_webmcp_call", Arguments: map[string]any{"pageId": page.PageID, "revision": catalog.Revision, "frameId": frameID, "toolName": "slow_tool", "input": map[string]any{"q": "x"}}}); err == nil {
		t.Fatal("cancelled MCP call completed")
	}
	started := time.Now()
	if _, failed := call(t, ctx, f, "artemis_observe", map[string]any{"pageId": page.PageID}); failed != "" || time.Since(started) > 10*time.Second {
		t.Fatalf("page stayed blocked after cancellation (%s): %s", time.Since(started), failed)
	}
}

func TestMCPRunBoundsMessageSize(t *testing.T) {
	agent, err := artemis.NewAgent(artemis.AgentConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := agent.Stop(); err != nil {
			t.Error(err)
		}
	})
	reader, writer := io.Pipe()
	output, sink := io.Pipe()
	go func() {
		if _, err := io.Copy(io.Discard, output); err != nil {
			t.Error(err)
		}
	}()
	done := make(chan error, 1)
	go func() { done <- New(agent, "test").Run(t.Context(), reader, sink) }()
	go func() {
		// A syntactically open JSON-RPC message that never ends.
		if _, err := writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"pad":"`)); err != nil {
			return
		}
		chunk := []byte(strings.Repeat("x", 1<<20))
		for written := 0; written <= MaxMessageBytes; written += len(chunk) {
			if _, err := writer.Write(chunk); err != nil {
				return
			}
		}
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("oversized message did not end the connection with a bound error: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("oversized message was buffered without bound")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}
