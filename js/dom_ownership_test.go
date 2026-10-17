package js

import "testing"

func TestDOMCallbacksRetireArgumentsAndReturnedHandles(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target" class="actual"><i>seed</i></div>`, nil)
	const exercise = `(() => {
		const target = document.getElementById('target');
		for (let index = 0; index < 50; index++) {
			if (target.tagName !== 'DIV' || target.nodeType !== 1 || target.firstChild.textContent !== 'seed') throw Error('node getters lost actual data');
			if (document.querySelector('#target') !== target || document.querySelectorAll('.actual').length !== 1 ||
				document.getElementsByTagName('i').length !== 1 || document.getElementsByClassName('actual')[0] !== target) throw Error('document lookup lost actual nodes');
			if (target.querySelector('i') !== target.firstChild || target.querySelectorAll('i').length !== 1 || target.childNodes.length !== 1) throw Error('node collection lost data');
			target.setAttribute('data-actual', 'kept');
			if (target.getAttribute('data-actual') !== 'kept' || !target.hasAttribute('data-actual')) throw Error('attribute write lost');
			target.removeAttribute('data-actual');
			target.firstChild.textContent = 'seed';
			target.style.setProperty('color', 'red');
			if (target.style.getPropertyValue('color') !== 'red') throw Error('style lost');
			target.style.removeProperty('color');
			if (target.getAttribute('absent') !== null || target.querySelector('absent') !== null || document.querySelector('absent') !== null) throw Error('null result changed');
			if (document.URL !== 'https://example.test/' || document.readyState !== 'loading' || document.title !== '') throw Error('document primitive changed');
		}
	})()`
	if err := c.EvalDiscard(t.Context(), exercise); err != nil {
		t.Fatal(err)
	}
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	for range 3 {
		if err := c.EvalDiscard(t.Context(), exercise); err != nil {
			t.Fatal(err)
		}
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("DOM callbacks retained native handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
}

func TestDOMCookieCallbacksRetireHandlesAndPreserveEffects(t *testing.T) {
	cookie := ""
	writes := 0
	c := newCtxFromHTMLOpts(t, `<div></div>`, ContextOpts{GetCookie: func() string { return cookie }, SetCookie: func(value string) { cookie = value; writes++ }})
	const exercise = `for (let index = 0; index < 50; index++) { document.cookie = 'actual=kept'; if (document.cookie !== 'actual=kept') throw Error('cookie effect lost'); }`
	if err := c.EvalDiscard(t.Context(), exercise); err != nil {
		t.Fatal(err)
	}
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	for range 3 {
		if err := c.EvalDiscard(t.Context(), exercise); err != nil {
			t.Fatal(err)
		}
	}
	if writes != 200 || cookie != "actual=kept" {
		t.Fatalf("actual cookie callbacks: writes=%d cookie=%q", writes, cookie)
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("cookie callbacks retained native handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
}

func TestDOMParserAndAttributeCollectionsRetireHandles(t *testing.T) {
	c := newCtxFromHTML(t, `<button onclick="this.textContent='clicked'">ready</button>`, nil)
	const exercise = `(() => {
		for (let index = 0; index < 50; index++) {
			const parsed = new DOMParser().parseFromString('<p>actual</p>', 'text/html');
			if (parsed.querySelector('p').textContent !== 'actual') throw Error('parsed content lost');
			const attributes = __list_on_attrs();
			if (attributes.length !== 1 || attributes[0].event !== 'click' || attributes[0].body !== "this.textContent='clicked'") throw Error('attribute tuple lost');
		}
	})()`
	if err := c.EvalDiscard(t.Context(), exercise); err != nil {
		t.Fatal(err)
	}
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	for range 3 {
		if err := c.EvalDiscard(t.Context(), exercise); err != nil {
			t.Fatal(err)
		}
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("DOM extras retained native handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
}

func TestDOMCollectionsIgnoreInheritedSettersAndRetireFailures(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target" onclick="void 0"><i>actual</i></div>`, nil)
	const exercise = `(() => {
		const target = document.getElementById('target');
		let intercepted = 0;
		for (const key of ['0', 'length', 'id', 'event', 'body']) {
			Object.defineProperty(Object.prototype, key, {configurable: true, set() { intercepted++; throw Error('inherited setter called'); }});
		}
		try {
			if (__doc_call('querySelectorAll', 'i')[0] !== __doc_call('querySelector', 'i')) throw Error('collection lost');
			if (__list_on_attrs()[0].event !== 'click') throw Error('attribute lost');
			if (intercepted !== 0) throw Error('inherited setter observed native output');
		} finally {
			for (const key of ['0', 'length', 'id', 'event', 'body']) delete Object.prototype[key];
		}
		for (let index = 0; index < 5000; index++) {
			let failed = false;
			try { __node_call(-1, 'appendChild', 1); } catch { failed = true; }
			if (!failed) throw Error('invalid target accepted');
			try { __node_call(__doc_call('getElementById', 'target'), 'removeChild', -1); throw Error('invalid child accepted'); }
			catch (error) { if (String(error).includes('invalid child accepted')) throw error; }
		}
	})()`
	if err := c.EvalDiscard(t.Context(), exercise); err != nil {
		t.Fatal(err)
	}
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	if err := c.EvalDiscard(t.Context(), exercise); err != nil {
		t.Fatal(err)
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("DOM failures retained native handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
}
