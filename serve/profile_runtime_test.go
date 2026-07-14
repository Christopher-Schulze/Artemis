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

	server := &Server{agent: agent, opts: Opts{}}
	params, _ := json.Marshal(SessionNewParams{ProfileID: "serve-profile", OwnerUserRef: "owner", Class: string(profile.ProfileEphemeral)})
	created := server.cmdSessionNew(context.Background(), &Request{ID: "1", Params: params})
	if !created.OK {
		t.Fatalf("create: %+v", created.Error)
	}
	value := created.Value.(SessionNewResult)
	id := value.SessionID
	if len(runtime.List("owner")) != 1 {
		t.Fatal("session absent from profile runtime")
	}
	closeParams, _ := json.Marshal(SessionCloseParams{SessionID: id, OwnerUserRef: "owner"})
	closed := server.cmdSessionClose(&Request{ID: "2", Params: closeParams})
	if !closed.OK {
		t.Fatalf("close: %+v", closed.Error)
	}
	state, err := runtime.Get(profile.SessionID(id), "owner")
	if err != nil || state.State != profile.SessionClosed {
		t.Fatalf("runtime close state: %+v err=%v", state, err)
	}
}
