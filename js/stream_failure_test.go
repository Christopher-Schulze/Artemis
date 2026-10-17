package js

import "testing"

func TestStreamCancelPreservesActualCallbackFailureAndClosesPendingReads(t *testing.T) {
	for _, callback := range []string{`throw failure`, `return Promise.reject(failure)`, `return {get then(){throw failure}}`} {
		t.Run(callback, func(t *testing.T) {
			c := newCtxFromHTML(t, `<body></body>`, nil)
			source := `globalThis.result='pending';globalThis.cancelCalls=0;const failure={actual:true};
const stream=new ReadableStream({cancel(reason){if(reason!=='actual-reason')throw Error('wrong cancel reason');cancelCalls++;` + callback + `}});
const reader=stream.getReader();let closed=false;
reader.read().then(value=>{closed=value.done===true&&value.value===undefined});
reader.cancel('actual-reason').then(()=>{result='false success'},async error=>{
if(error!==failure)throw Error('cancel reason changed');await reader.cancel('actual-reason');
result=closed&&cancelCalls===1?'actual failure':'pending read or duplicate cancel';});`
			if err := c.EvalDiscard(t.Context(), source); err != nil {
				t.Fatal(err)
			}
			if got, err := c.EvalString(t.Context(), `result`); err != nil || got != "actual failure" {
				t.Fatalf("cancel lost callback failure or pending ownership: %q %v", got, err)
			}
		})
	}
}

func TestStreamAbortPreservesActualSinkFailureThroughBothEntryPoints(t *testing.T) {
	for _, entry := range []string{"stream", "writer"} {
		for _, callback := range []string{`throw failure`, `return Promise.reject(failure)`} {
			t.Run(entry+callback, func(t *testing.T) {
				c := newCtxFromHTML(t, `<body></body>`, nil)
				writerSetup := "let writer;"
				if entry == "writer" {
					writerSetup += "writer=stream.getWriter();"
				}
				source := `globalThis.result='pending';let calls=0;const failure={actual:true};const reason={original:true};
const stream=new WritableStream({abort(value){if(value!==reason)throw Error('wrong abort reason');calls++;` + callback + `}});
` + writerSetup + entry + `.abort(reason).then(()=>{result='false success'},async error=>{
if(error!==failure)throw Error('abort failure changed');if(!writer)writer=stream.getWriter();await writer.abort(reason);
try{await writer.write('must not write');throw Error('write succeeded')}catch(value){if(value!==reason)throw value}
result=calls===1?'actual failure':'duplicate abort';});`
				if err := c.EvalDiscard(t.Context(), source); err != nil {
					t.Fatal(err)
				}
				if got, err := c.EvalString(t.Context(), `result`); err != nil || got != "actual failure" {
					t.Fatalf("abort discarded sink failure: %q %v", got, err)
				}
			})
		}
	}
}

func TestStreamErrorStatePreservesEveryFalseyReason(t *testing.T) {
	for _, reason := range []string{"undefined", "null", "false", "0", "-0", "NaN", "0n", `''`} {
		for _, source := range []string{
			`new ReadableStream({start(){throw reason}}).getReader().read()`,
			`new ReadableStream({pull(){throw reason}}).getReader().read()`,
			`new ReadableStream({start(c){c.enqueue('stale');c.error(reason)}}).getReader().read()`,
			`new ReadableStream({start(c){c.enqueue('stale');c.close();c.error(reason)}}).getReader().read()`,
			`new WritableStream({start(c){c.error(reason)}}).getWriter().write('must not write')`,
			`new WritableStream({start(c){c.error(reason)}}).getWriter().close()`,
		} {
			t.Run(reason+source, func(t *testing.T) {
				c := newCtxFromHTML(t, `<body></body>`, nil)
				if err := c.EvalDiscard(t.Context(), `globalThis.result='pending';const reason=`+reason+`;`+source+`.then(()=>{result='false success'},error=>{result=Object.is(error,reason)?'exact failure':'changed failure'})`); err != nil {
					t.Fatal(err)
				}
				if got, err := c.EvalString(t.Context(), `result`); err != nil || got != "exact failure" {
					t.Fatalf("falsey stream error became success: %q %v", got, err)
				}
			})
		}
	}
}

func TestStreamCleanupWaitsForActualCallbackSettlement(t *testing.T) {
	for _, operation := range []string{"cancel", "abort"} {
		t.Run(operation, func(t *testing.T) {
			c := newCtxFromHTML(t, `<body></body>`, nil)
			source := `globalThis.result='pending';globalThis.calls=0;globalThis.readClosed=false;globalThis.finish=null;const reason={actual:true};
const cleanup=value=>{if(value!==reason)throw Error('cleanup reason changed');calls++;return new Promise(resolve=>finish=resolve)};`
			if operation == "cancel" {
				source += `const stream=new ReadableStream({cancel:cleanup});const reader=stream.getReader();reader.read().then(value=>readClosed=value.done===true);reader.cancel(reason).then(value=>{result=value===undefined?'settled':'wrong value'});`
			} else {
				source += `const stream=new WritableStream({abort:cleanup});const writer=stream.getWriter();const first=writer.abort(reason),second=writer.abort(reason);if(first!==second)throw Error('pending abort lost its owner');first.then(value=>{result=value===undefined?'settled':'wrong value'});`
			}
			if err := c.EvalDiscard(t.Context(), source); err != nil {
				t.Fatal(err)
			}
			if got, err := c.EvalString(t.Context(), `result+':'+calls`); err != nil || got != "pending:1" {
				t.Fatalf("cleanup acknowledged before actual settlement: %q %v", got, err)
			}
			if operation == "cancel" {
				if got, err := c.EvalString(t.Context(), `String(readClosed)`); err != nil || got != "true" {
					t.Fatalf("cancel retained a pending reader: %q %v", got, err)
				}
			}
			if err := c.EvalDiscard(t.Context(), `finish('discard actual callback return value')`); err != nil {
				t.Fatal(err)
			}
			if got, err := c.EvalString(t.Context(), `result+':'+calls`); err != nil || got != "settled:1" {
				t.Fatalf("cleanup settlement lost its actual result: %q %v", got, err)
			}
		})
	}
}

func TestStreamCancelDiscardsQueuedDataAfterCloseWasRequested(t *testing.T) {
	c := newCtxFromHTML(t, `<body></body>`, nil)
	if err := c.EvalDiscard(t.Context(), `
globalThis.result='pending';let calls=0;
const stream=new ReadableStream({start(c){c.enqueue('must be discarded');c.close()},cancel(){calls++}});
const reader=stream.getReader();
reader.cancel().then(async value=>{
const next=await reader.read();await reader.cancel();
result=value===undefined&&next.done&&next.value===undefined&&calls===1?'closed':'stale data or cleanup';
});`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), `result`); err != nil || got != "closed" {
		t.Fatalf("close-requested stream bypassed cancellation: %q %v", got, err)
	}
}
