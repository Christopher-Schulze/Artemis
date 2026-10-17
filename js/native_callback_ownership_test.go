package js

import (
	"strings"
	"testing"
)

func TestConsoleURLAndComputedStyleCallbacksRetireNativeHandles(t *testing.T) {
	for _, test := range []struct {
		name, script string
		logs         int
	}{
		{"console", `for (let index=0;index<50;index++) { console.log('actual', 7); console.warn('actual', 7); console.error('actual', 7); console.info('actual', 7); console.debug('actual', 7); }`, 1000},
		{"URL", `for (let index=0;index<50;index++) { const value=new URL('/actual?q=kept#fragment','https://example.test/base'); if(value.href!=='https://example.test/actual?q=kept#fragment'||value.hostname!=='example.test')throw Error('actual URL lost'); }`, 0},
		{"computed style", `for (let index=0;index<50;index++) { if(getComputedStyle(document.getElementById('target')).color!=='rgb(255, 0, 0)')throw Error('actual computed style lost'); }`, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			logs := 0
			c := newCtxFromHTML(t, `<div id="target" style="color:red"></div>`, FuncConsole(func(level, message string) {
				logs++
				if message != "actual 7" {
					t.Errorf("console lost actual arguments: %q", message)
				}
			}))
			if err := c.EvalDiscard(t.Context(), test.script); err != nil {
				t.Fatal(err)
			}
			pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
			for range 3 {
				if err := c.EvalDiscard(t.Context(), test.script); err != nil {
					t.Fatal(err)
				}
			}
			if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
				t.Fatalf("callbacks retained handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
			}
			if logs != test.logs {
				t.Fatalf("sink calls=%d want %d", logs, test.logs)
			}
		})
	}
}

func TestConsoleRejectsConversionFailureAndQuotaBeforePublishing(t *testing.T) {
	logs := 0
	c := newCtxFromHTML(t, `<div></div>`, FuncConsole(func(string, string) { logs++ }))
	for _, script := range []string{
		`console.log('must not publish', {toString(){throw Error('actual console conversion failure')}})`,
		`console.warn('x'.repeat(1048577))`,
		`console.info('x'.repeat(1048576), '')`,
		`console.error('é'.repeat(524289))`,
		`console.debug(...Array(4097).fill(''))`,
	} {
		if err := c.EvalDiscard(t.Context(), script); err == nil {
			t.Fatalf("invalid console call acknowledged success: %s", script)
		}
	}
	if logs != 0 {
		t.Fatalf("failed console arguments reached sink: %d", logs)
	}
	if err := c.EvalDiscard(t.Context(), `console.log('actual recovery')`); err != nil {
		t.Fatal(err)
	}
	if logs != 1 {
		t.Fatalf("recovery did not publish: %d", logs)
	}
}

func TestURLNativeOutputIgnoresInheritedSettersAndReportsConversionFailures(t *testing.T) {
	c := newCtxFromHTML(t, `<div></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `(() => {
		let intercepted = 0;
		Object.defineProperty(Object.prototype, 'href', {configurable:true, set(){intercepted++;throw Error('inherited URL setter')}});
		try {
			const value = __url_parse('/actual', 'https://example.test/base');
			if (value.href !== 'https://example.test/actual' || intercepted !== 0) throw Error('native URL construction intercepted');
		} finally { delete Object.prototype.href; }
	})()`); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{
		`__url_parse({toString(){throw Error('actual href conversion failure')}})`,
		`__url_parse('/actual', {toString(){throw Error('actual base conversion failure')}})`,
	} {
		if err := c.EvalDiscard(t.Context(), script); err == nil || !strings.Contains(err.Error(), "conversion failure") {
			t.Fatalf("URL conversion error lost: %v", err)
		}
	}
}

func TestComputedStyleFailuresRetireNativeHandles(t *testing.T) {
	valid := newCtxFromHTML(t, `<div id="target" style="color:red"></div>`, nil)
	if err := valid.EvalDiscard(t.Context(), `(() => {
		const id = __doc_call('getElementById', 'target');
		for (const read of [
			() => __cascade_style({valueOf(){throw Error('must not coerce')}}, 'color'),
			() => __cascade_style(1.5, 'color'),
			() => __cascade_style(id, {toString(){throw Error('actual property conversion failure')}}),
		]) {
			let rejected=false;
			try { read(); } catch { rejected=true; }
			if (!rejected) throw Error('invalid native computed-style input accepted');
		}
		if (getComputedStyle(document.getElementById('target')).color !== 'rgb(255, 0, 0)') throw Error('actual style damaged');
	})()`); err != nil {
		t.Fatal(err)
	}
	c := newCtxFromHTML(t, `<div id="target" style="color:red;broken"></div>`, nil)
	const script = `try { getComputedStyle(document.getElementById('target')).color; } catch {}`
	if err := c.EvalDiscard(t.Context(), script); err == nil {
		t.Fatal("malformed style accepted")
	}
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	for range 50 {
		if err := c.EvalDiscard(t.Context(), script); err == nil {
			t.Fatal("caught malformed style acknowledged success")
		}
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("computed-style failures retained handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
}
