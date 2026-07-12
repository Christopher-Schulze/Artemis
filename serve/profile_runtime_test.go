package serve

import (
	"encoding/json"
	"testing"

	"github.com/Christopher-Schulze/Artemis/profile"
)

func TestServeSessionUsesAuthoritativeProfileRuntime(t *testing.T) {
	runtime, err := profile.NewRuntimeManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{opts: Opts{ProfileRuntime: runtime}, sessions: make(map[string]*session)}
	params, _ := json.Marshal(SessionNewParams{ProfileID: "serve-profile", OwnerUserRef: "owner", Class: string(profile.ProfileEphemeral)})
	created := server.cmdSessionNew(&Request{ID: "1", Params: params})
	if !created.OK {
		t.Fatalf("create: %+v", created.Error)
	}
	value := created.Value.(map[string]any)
	id := value["sessionId"].(string)
	if len(runtime.List("owner")) != 1 {
		t.Fatal("session absent from profile runtime")
	}
	closeParams, _ := json.Marshal(SessionCloseParams{SessionID: id})
	closed := server.cmdSessionClose(&Request{ID: "2", Params: closeParams})
	if !closed.OK {
		t.Fatalf("close: %+v", closed.Error)
	}
	state, err := runtime.Get(profile.SessionID(id), "owner")
	if err != nil || state.State != profile.SessionClosed {
		t.Fatalf("runtime close state: %+v err=%v", state, err)
	}
}
