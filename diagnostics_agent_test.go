package artemis

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Artemis/diagnostics"
	"github.com/Christopher-Schulze/Artemis/network"
)

func TestAgentDiagnosticsUseTriggeringSessionReference(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "agent-diagnostics-body")
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := NewAgent(AgentConfig{PolicyConfig: network.PolicyConfig{
		AllowPrivateNetworks: true,
		AllowedPorts:         []int{80, 443, port},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := agent.Stop(); err != nil {
			t.Errorf("stop: %v", err)
		}
	}()
	session, err := agent.CreateSession("owner")
	if err != nil {
		t.Fatal(err)
	}
	pageID, page, taskErr := session.OpenPage(context.Background(), server.URL+"/private?token=secret", false)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	download, err := page.SaveDownload("proof.txt")
	if err != nil {
		t.Fatal(err)
	}
	if download.Size == 0 {
		t.Fatal("download resource evidence missing")
	}
	if taskErr := session.ClosePage(pageID); taskErr != nil {
		t.Fatal(taskErr)
	}
	records, err := agent.Diagnostics()
	if err != nil {
		t.Fatal(err)
	}
	want := diagnostics.HashSession(session.SessionID())
	var policySeen, resourceSeen, diskSeen bool
	for _, record := range records {
		if record.Policy != nil && record.Policy.SessionRef == want {
			policySeen = true
		}
		if record.Resource != nil && record.Resource.SessionRef == want {
			resourceSeen = true
			diskSeen = diskSeen || record.Resource.DiskBytes >= download.Size
		}
	}
	if !policySeen || !resourceSeen || !diskSeen {
		t.Fatalf("session diagnostics policy=%v resource=%v disk=%v records=%+v", policySeen, resourceSeen, diskSeen, records)
	}
	encoded := fmt.Sprint(records)
	for _, forbidden := range []string{session.SessionID(), "/private", "token=secret", "agent-diagnostics-body"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("diagnostics leaked %q: %s", forbidden, encoded)
		}
	}
}
