package serve

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Christopher-Schulze/Artemis/bridge/actions"
)

type actionExecutorFunc func(context.Context, actions.Request) actions.Outcome

func (f actionExecutorFunc) Execute(ctx context.Context, request actions.Request) actions.Outcome {
	return f(ctx, request)
}

func TestChromiumActDispatchesCanonicalRequest(t *testing.T) {
	server := &Server{opts: Opts{ChromiumActions: actionExecutorFunc(func(_ context.Context, request actions.Request) actions.Outcome {
		return actions.Outcome{Success: request.Kind == actions.KindScreenshot, Evidence: actions.Evidence{Action: request.Kind}}
	})}}
	params, _ := json.Marshal(ChromiumActParams{Request: actions.Request{Kind: actions.KindScreenshot}})
	response := server.dispatch(context.Background(), &Request{ID: "1", Cmd: string(CmdChromiumAct), Params: params})
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
	server := &Server{}
	response := server.dispatch(context.Background(), &Request{ID: "1", Cmd: string(CmdChromiumAct)})
	if response.OK || response.Error == nil || response.Error.Code != "no_page" {
		t.Fatalf("response=%#v", response)
	}
}
