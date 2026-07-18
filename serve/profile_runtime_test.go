package serve

import (
	"context"
	"encoding/json"
	"testing"

	artemis "github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/profile"
)

func TestServeSessionUsesAuthoritativeProfileRuntime(t *testing.T) {
	runtime, err := profile.NewRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	agent, err := artemis.NewAgent(artemis.AgentConfig{PolicyConfig: network.PolicyConfig{AllowPrivateNetworks: true, AllowedPorts: allTestPorts()}})
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := agent.Start(context.Background()); err != nil {
		t.Fatalf("agent start: %v", err)
	}
	defer agent.Stop()
	agent.SetProfileRuntime(runtime)

	server := New(agent, Opts{AuthToken: testAuthToken})
	client := clientIdentity{id: "client.test", ownerRef: "serve:test", rateKey: "127.0.0.1"}
	params, _ := json.Marshal(SessionNewParams{ProfileID: "serve-profile", Class: string(profile.ProfileEphemeral)})
	created := server.cmdSessionNew(context.Background(), client, &Request{ID: "1", Params: params})
	if !created.OK {
		t.Fatalf("create: %+v", created.Error)
	}
	value := created.Value.(SessionNewResult)
	id := value.SessionID
	if len(runtime.List(client.ownerRef)) != 1 {
		t.Fatal("session absent from profile runtime")
	}
	closeParams, _ := json.Marshal(SessionCloseParams{SessionID: id})
	closed := server.cmdSessionClose(client, &Request{ID: "2", Params: closeParams})
	if !closed.OK {
		t.Fatalf("close: %+v", closed.Error)
	}
	state, err := runtime.Get(profile.SessionID(id), client.ownerRef)
	if err != nil || state.State != profile.SessionClosed {
		t.Fatalf("runtime close state: %+v err=%v", state, err)
	}
}
