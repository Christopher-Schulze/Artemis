package fixture

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/engine"
)

func TestNativeFetchAbortAfterCompletedTransportKeepsBodiesUndisturbed(t *testing.T) {
	server := NewServer()
	defer server.Close()
	if err := server.RegisterHTML("/abort-native", "<html><body>native abort observation</body></html>"); err != nil {
		t.Fatal(err)
	}
	if err := server.RegisterRaw("/abort-body", "text/plain", 200, []byte("actual body")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	native := &bridgeRunner{}
	defer native.stop(context.Background(), t)
	native.start(ctx, t, server)
	scenario := Scenario{Expect: Expect{Status: 200, Contains: []string{"native abort observation"}}}
	navigation, err := RunNativeScenario(ctx, native.page, scenario, server.URL("/abort-native"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateCrossResult(scenario, navigation); err != nil {
		t.Fatal(err)
	}
	var version struct {
		Product  string `json:"product"`
		Revision string `json:"revision"`
	}
	if err := native.page.CallBrowser(ctx, "Browser.getVersion", struct{}{}, &version); err != nil {
		t.Fatal(err)
	}
	t.Logf("native product=%s revision=%s", version.Product, version.Revision)
	const expression = `(async()=>{
		const controller=new AbortController(), reason={actual:42};
		const original=await fetch('/abort-body',{signal:controller.signal}), clone=original.clone();
		const completed=await original.clone().text();
		if(completed!=='actual body') throw Error('actual transport completion not proven');
		controller.abort(reason);
		let originalResult;
		try{await original.text();originalResult='invalid success'}catch(error){originalResult=error===reason?'exact reason':String(error)}
		let cloneResult;
		try{cloneResult=await clone.text()}catch(error){cloneResult=error===reason?'exact reason':'distinct:'+JSON.stringify(error)}
		return JSON.stringify({completed,originalResult,cloneResult,originalUsed:original.bodyUsed,cloneUsed:clone.bodyUsed});
	})()`
	actual := evaluateNativeAbort(t, ctx, native.page, expression)
	want := `{"completed":"actual body","originalResult":"exact reason","cloneResult":"exact reason","originalUsed":false,"cloneUsed":false}`
	if actual != want {
		t.Fatalf("actual native completed-transport clone/abort semantics: result=%s", actual)
	}
	if result := evaluateAbortOnRenderless(t, ctx, server, expression); result != actual {
		t.Fatalf("renderless abort contradicts actual native result: renderless=%s native=%s", result, actual)
	}
	for _, status := range []int{200, 204} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			path := fmt.Sprintf("/abort-empty-%d", status)
			if err := server.RegisterRaw(path, "text/plain", status, nil); err != nil {
				t.Fatal(err)
			}
			if status == 204 {
				shape := evaluateNativeAbort(t, ctx, native.page, `(async()=>{const response=await fetch('/abort-empty-204');return JSON.stringify({status:response.status,bodyNull:response.body===null,stream:response.body instanceof ReadableStream})})()`)
				if shape != `{"status":204,"bodyNull":false,"stream":true}` {
					t.Fatalf("actual Chromium null-status body kind changed: %s", shape)
				}
			}
			for _, readFirst := range []bool{false, true} {
				expression := fmt.Sprintf(`(async()=>{
				const controller=new AbortController(), reason={actual:42}, original=await fetch(%q,{signal:controller.signal}), clone=original.clone();
				if(original.status!==%d) throw Error('actual response status not proven: '+original.status);
				if(await original.clone().text()!=='') throw Error('empty transport completion not proven');
				if(%t){await original.text();await clone.text()}
				controller.abort(reason);
				const output=[];
				for(const body of [original,clone]){try{output.push('empty:'+await body.text())}catch(error){output.push(error===reason?'exact reason':error instanceof TypeError?'used':String(error))}output.push(body.bodyUsed)}
				return JSON.stringify(output);
			})()`, path, status, readFirst)
				nativeResult := evaluateNativeAbort(t, ctx, native.page, expression)
				want := `["exact reason",false,"exact reason",false]`
				if readFirst {
					want = `["used",true,"used",true]`
				}
				if nativeResult != want {
					t.Fatalf("native empty/null abort semantics: status=%d readFirst=%t got=%s want=%s", status, readFirst, nativeResult, want)
				}
				renderlessWant := nativeResult
				if status == 204 {
					// Keep the Fetch null-body contract explicit. Chromium instead
					// exposes an empty stream: https://github.com/whatwg/fetch/issues/1779.
					// This boundary cannot substantiate browser-equivalent capability.
					renderlessWant = `["empty:",false,"empty:",false]`
				}
				if actual := evaluateAbortOnRenderless(t, ctx, server, expression); actual != renderlessWant {
					t.Errorf("empty/null renderless contract failed: status=%d readFirst=%t renderless=%s want=%s native=%s", status, readFirst, actual, renderlessWant, nativeResult)
				}
			}
		})
	}
}

func evaluateNativeAbort(t *testing.T, ctx context.Context, page *bridge.Page, expression string) string {
	t.Helper()
	var observed struct {
		Result struct {
			Value       json.RawMessage `json:"value"`
			Description string          `json:"description"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	parameters := struct {
		Expression    string `json:"expression"`
		ReturnByValue bool   `json:"returnByValue"`
		AwaitPromise  bool   `json:"awaitPromise"`
	}{expression, true, true}
	if err := page.Call(ctx, "Runtime.evaluate", parameters, &observed); err != nil {
		t.Fatal(err)
	}
	if len(observed.Exception) != 0 && string(observed.Exception) != "null" {
		t.Fatalf("actual native abort evaluation failed: result=%s description=%s exception=%s", observed.Result.Value, observed.Result.Description, observed.Exception)
	}
	var value string
	if err := json.Unmarshal(observed.Result.Value, &value); err != nil {
		t.Fatalf("native abort result is not the actual string outcome: raw=%s description=%s err=%v", observed.Result.Value, observed.Result.Description, err)
	}
	return value
}

func evaluateAbortOnRenderless(t *testing.T, ctx context.Context, server *Server, expression string) string {
	t.Helper()
	renderless := &engineRunner{}
	renderless.start(ctx, t, server)
	defer renderless.stop(context.Background(), t)
	page, err := renderless.eng.Fetch(ctx, server.URL("/abort-native"), engine.FetchOpts{AsyncFetch: true})
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestResource(t, "renderless abort page", page.Close)
	if _, err := page.EvalString(ctx, `globalThis.actualAbortResult='pending'; `+expression+`.then(value=>{actualAbortResult=value},error=>{actualAbortResult='failure:'+String(error)}); 'started'`); err != nil {
		t.Fatal(err)
	}
	if err := page.WaitIdle(ctx); err != nil {
		t.Fatal(err)
	}
	actual, err := page.EvalString(ctx, "actualAbortResult")
	if err != nil {
		t.Fatal(err)
	}
	return actual
}
