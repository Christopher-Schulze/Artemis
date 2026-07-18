package serve

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/coder/websocket"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	"github.com/Christopher-Schulze/Artemis/network"
)

type actionExecutorFunc func(context.Context, actions.Request) actions.Outcome

func (f actionExecutorFunc) Execute(ctx context.Context, request actions.Request) actions.Outcome {
	return f(ctx, request)
}

func testChromiumAgent(t *testing.T) *artemis.Agent {
	t.Helper()
	agent, err := artemis.NewAgent(artemis.AgentConfig{PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: allTestPorts()}})
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("agent start: %v", err)
	}
	return agent
}

func TestChromiumActDispatchesCanonicalRequest(t *testing.T) {
	agent := testChromiumAgent(t)
	defer agent.Stop()
	agent.SetChromiumActions(actionExecutorFunc(func(_ context.Context, request actions.Request) actions.Outcome {
		return actions.Outcome{Success: request.Kind == actions.KindScreenshot, Evidence: actions.Evidence{Action: request.Kind}}
	}))

	session, err := agent.CreateSession("test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	server := New(agent, Opts{AuthToken: testAuthToken})
	client := clientIdentity{id: "client.test", ownerRef: "test", rateKey: "127.0.0.1"}
	server.trackSession(session.SessionID(), client.id)
	params, _ := json.Marshal(ChromiumActParams{SessionID: session.SessionID(), Request: actions.Request{Kind: actions.KindScreenshot}})
	response := server.dispatch(context.Background(), context.Background(), (*websocket.Conn)(nil), client, &Request{ID: "1", Cmd: string(CmdChromiumAct), Params: params}, nil)
	if !response.OK {
		t.Fatalf("response=%#v", response)
	}
	raw, _ := json.Marshal(response.Value)
	var result ChromiumActResult
	if err := json.Unmarshal(raw, &result); err != nil || !result.Outcome.Success {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestChromiumActFailsClosedWithoutRuntime(t *testing.T) {
	agent := testChromiumAgent(t)
	defer agent.Stop()
	session, err := agent.CreateSession("test")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	server := New(agent, Opts{AuthToken: testAuthToken})
	client := clientIdentity{id: "client.test", ownerRef: "test", rateKey: "127.0.0.1"}
	server.trackSession(session.SessionID(), client.id)
	params, _ := json.Marshal(ChromiumActParams{SessionID: session.SessionID(), Request: actions.Request{Kind: actions.KindScreenshot}})
	response := server.dispatch(context.Background(), context.Background(), (*websocket.Conn)(nil), client, &Request{ID: "1", Cmd: string(CmdChromiumAct), Params: params}, nil)
	if response.OK || response.Error == nil || response.Error.Code != "capability_unavailable" {
		t.Fatalf("response=%#v", response)
	}
}
