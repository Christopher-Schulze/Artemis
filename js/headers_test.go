package js

import (
	"context"
	"testing"
)

func TestHeadersValidationIterationAndPrivateState(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const h=new Headers([['Z',' last '],['X-Actual','first'],['x-actual','second'],['Set-Cookie','a=1'],['set-cookie','b=2']]);
		const initial=JSON.stringify([...h]);
		const cookies=h.getSetCookie(); cookies.push('forged');
		h._k=['forged']; h._v={'x-actual':'forged'};
		const iterator=h.entries(); iterator.next(); h.set('A','new');
		const rest=[...iterator].map(([k])=>k).join(',');
		let rejected=0;
		for(const action of [()=>new Headers([['only']]),()=>new Headers({'Bad Name':'x'}),()=>h.set('x','a\r\nb'),()=>h.get(''),()=>h.append('x','\u0100'),()=>h.append('x'),()=>Headers.prototype.get.call({},'x')]) {
			try {action()} catch(e) {if(e instanceof TypeError) rejected++}
		}
		return [initial,h.get('X-ACTUAL'),h.getSetCookie().join(';'),rest,rejected].join('|');
	})()`)
	want := `[["set-cookie","a=1"],["set-cookie","b=2"],["x-actual","first, second"],["z","last"]]|first, second|a=1;b=2|set-cookie,set-cookie,x-actual,z|7`
	if err != nil || got != want {
		t.Fatalf("headers lost validation, ordering or private state: %q %v", got, err)
	}
}

func TestFetchHeadersUseCanonicalInputsAndImmutableResponseGuard(t *testing.T) {
	for _, input := range []string{`new Headers([['X-Actual',' first '],['x-actual','second']])`, `[['X-Actual','first'],['x-actual',' second ']]`, `{'X-Actual':'first','x-actual':'second'}`} {
		t.Run(input, func(t *testing.T) {
			var got FetchRequest
			c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Fetch: func(_ context.Context, request FetchRequest) (*FetchResponse, error) {
				got = request
				return &FetchResponse{Status: 200, Headers: map[string][]string{"X-Actual": {"owned"}}}, nil
			}})
			if err := c.EvalDiscard(t.Context(), `globalThis.observed=''; fetch('/headers',{headers:`+input+`}).then(r=>{
				let rejected=0;
				for(const headers of [r.headers,r.clone().headers]) for(const action of [()=>headers.append('x','bad'),()=>headers.set('x','bad'),()=>headers.delete('x-actual')]) {
					try{action()}catch(e){if(e instanceof TypeError)rejected++}
				}
				observed=r.headers.get('X-ACTUAL')+':'+rejected;
			});`); err != nil {
				t.Fatal(err)
			}
			value, err := c.EvalString(t.Context(), "observed")
			if err != nil || value != "owned:6" || len(got.Headers) != 1 || !(got.Headers["X-Actual"] == "first, second" || got.Headers["x-actual"] == "first, second") {
				t.Fatalf("header transport or response guard failed: %#v %q %v", got.Headers, value, err)
			}
		})
	}
}
