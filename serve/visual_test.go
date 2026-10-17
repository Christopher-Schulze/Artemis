package serve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

func TestChromiumVisualCommandsBindLabelAndGateActions(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeTestResponse(t, w, `<!doctype html><body style="margin:0"><button style="position:absolute;left:10px;top:10px;width:100px;height:30px" onclick="document.title='pressed'">Go</button></body>`)
	}))
	defer page.Close()
	parsed, err := url.Parse(page.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := artemis.NewAgent(artemis.AgentConfig{Chromium: &browserprocess.LaunchConfig{Headless: true}, PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: []int{port}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer stopTestAgent(t, agent)
	session, err := agent.CreateSession("visual")
	if err != nil {
		t.Fatal(err)
	}
	pageID, _, taskErr := session.OpenChromiumPage(t.Context(), page.URL)
	if taskErr != nil {
		t.Fatal(taskErr)
	}
	server := New(agent, Opts{AuthToken: testAuthToken})
	client := clientIdentity{id: "client.test", ownerRef: "test", rateKey: "127.0.0.1"}
	server.trackSession(session.SessionID(), client.id)

	resp := dispatchTyped(t, server, client, "1", CmdChromiumVisual, ChromiumVisualParams{SessionID: session.SessionID(), PageID: pageID, Options: artemis.VisualCaptureOptions{Annotate: true}})
	var observation artemis.VisualObservation
	mustDecode(t, *resp, &observation)
	assertTrust(t, "chromium.visual", resp.Trust, strings.TrimSuffix(page.URL, "/"), "chromium.visual", false)
	if observation.ID == "" || len(observation.Image) == 0 || len(observation.Annotations) != 1 || observation.Annotations[0].Name != "Go" {
		t.Fatalf("visual observation = %+v", observation)
	}
	box := observation.Annotations[0].Box
	resp = dispatchTyped(t, server, client, "2", CmdChromiumVisualAct, ChromiumVisualActParams{SessionID: session.SessionID(), PageID: pageID, Request: artemis.VisualActionRequest{ObservationID: observation.ID, Kind: actions.KindClick, X: box.X + box.Width/2, Y: box.Y + box.Height/2}})
	var result artemis.VisualActionResult
	mustDecode(t, *resp, &result)
	if result.Path != artemis.VisualPathReference || result.Ref != observation.Annotations[0].Ref {
		t.Fatalf("visual act result = %+v", result)
	}
	if resp := dispatchTyped(t, server, client, "3", CmdChromiumVisualAct, ChromiumVisualActParams{SessionID: session.SessionID(), PageID: pageID, Request: artemis.VisualActionRequest{ObservationID: observation.ID, Kind: actions.KindClick, X: 5, Y: 5}}); resp.OK || resp.Error.Code != string(ErrStaleReference) {
		t.Fatalf("consumed observation reused over the wire: %+v", resp)
	}
	if resp := dispatchTyped(t, server, client, "4", CmdChromiumVisualGround, ChromiumVisualGroundParams{SessionID: session.SessionID(), PageID: pageID, ObservationID: observation.ID, Instruction: "the go button"}); resp.OK || resp.Error.Code != string(ErrCapabilityUnavailable) {
		t.Fatalf("grounding without a host model: %+v", resp)
	}
	if resp := dispatchTyped(t, server, client, "5", CmdChromiumAct, ChromiumActParams{SessionID: session.SessionID(), PageID: pageID, Request: actions.Request{Kind: actions.KindClick, Pointer: &actions.PointerTarget{X: 20, Y: 20}}}); resp.OK || resp.Error.Code != string(ErrBadParams) {
		t.Fatalf("unbound pointer accepted over the wire: %+v", resp)
	}
}
