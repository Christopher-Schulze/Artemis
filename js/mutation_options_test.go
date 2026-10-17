package js

import (
	"strings"
	"testing"
)

func TestMutationObserverSharesOrderedRecordsAcrossTargets(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="root"><i id="a"></i><b id="b"></b></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `
		globalThis.sharedTrace = [];
		const a = document.getElementById('a'), b = document.getElementById('b');
		const observer = new MutationObserver(function(records, owner) {
			if (owner !== observer || this !== observer) throw Error('callback receiver lost');
			sharedTrace.push(records.map(record => record.target.id).join(','));
		});
		observer.observe(b, {attributes: true}); observer.observe(a, {attributes: true});
		observer.observe(document.getElementById('root'), {attributes: true, subtree: true});
		b.setAttribute('data-first', 'one'); a.setAttribute('data-next', 'two'); b.setAttribute('data-last', 'three');
	`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), "sharedTrace.join(';')"); err != nil || got != "b,a,b" {
		t.Fatalf("multi-target observer split, reordered or duplicated delivery: %q %v", got, err)
	}
}

func TestMutationObserverTakeRecordsSharesChronologicalQueue(t *testing.T) {
	c := newCtxFromHTML(t, `<i id="a"></i><b id="b"></b>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const a = document.getElementById('a'), b = document.getElementById('b');
		const observer = new MutationObserver(() => { throw Error('drained records redelivered'); });
		observer.observe(b, {attributes: true}); observer.observe(a, {attributes: true});
		b.setAttribute('data-first', 'one'); a.setAttribute('data-next', 'two'); b.setAttribute('data-last', 'three');
		const records = observer.takeRecords(); observer.disconnect();
		return records.map(record => record.target.id).join(',');
	})()`)
	if err != nil || got != "b,a,b" {
		t.Fatalf("takeRecords grouped records by registration: %q %v", got, err)
	}
}

func TestMutationObserverReobserveReplacesOptionsWithoutDiscardingQueue(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const target = document.getElementById('target'), observer = new MutationObserver(() => {});
		observer.observe(target, {attributes: true}); target.setAttribute('data-kept', 'one');
		observer.observe(target, {childList: true}); target.setAttribute('data-ignored', 'two');
		target.appendChild(document.createElement('i'));
		const records = observer.takeRecords(); observer.disconnect();
		return records.map(record => record.type + ':' + (record.attributeName || '')).join(',');
	})()`)
	if err != nil || got != "attributes:data-kept,childList:" {
		t.Fatalf("reobserve duplicated registration or lost queued state: %q %v", got, err)
	}
}

func TestMutationObserverValidatesPublicOptionsBeforeChangingOwnership(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const target = document.getElementById('target'), observer = new MutationObserver(() => {});
		observer.observe(target, {attributes: true});
		let rejected = 0;
		for (const call of [() => new MutationObserver(null), () => observer.observe(target),
			() => observer.observe(target, {subtree: true}), () => observer.observe(target, {attributes: false, attributeOldValue: true}),
			() => observer.observe(target, {attributes: false, attributeFilter: []}),
			() => observer.observe(target, {characterData: false, characterDataOldValue: true}),
			() => observer.observe(target, {attributeFilter: null}), () => observer.observe(target, {attributeFilter: [Symbol('bad')]}),
			() => MutationObserver.prototype.disconnect.call({}), () => MutationObserver.prototype.takeRecords.call({})]) {
			try { call(); } catch (error) { if (!(error instanceof TypeError)) throw error; rejected++; }
		}
		target.setAttribute('data-kept', 'actual'); const records = observer.takeRecords(); observer.disconnect();
		return String(rejected === 10 && records.length === 1 && records[0].attributeName === 'data-kept');
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("invalid options/receiver changed actual observer ownership: %q %v", got, err)
	}
}

func TestMutationObserverFiltersAndOldValuesArePerObserver(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target" data-match="seed"></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const target = document.getElementById('target');
		const old = new MutationObserver(() => {}), plain = new MutationObserver(() => {}), empty = new MutationObserver(() => {});
		const filter = new Set(['data-match']);
		old.observe(target, {attributeFilter: filter, attributeOldValue: true});
		plain.observe(target, {attributes: true}); empty.observe(target, {attributeFilter: []});
		filter.clear(); filter.add('data-ignored'); target.setAttribute('data-match', 'next'); target.setAttribute('data-ignored', 'other');
		target.removeAttribute('data-match');
		const records = old.takeRecords(), publicRecords = plain.takeRecords(), emptyRecords = empty.takeRecords();
		old.disconnect(); plain.disconnect(); empty.disconnect();
		return String(records.length === 2 && records[0].oldValue === 'seed' && records[1].oldValue === 'next' &&
			publicRecords.length === 3 && publicRecords.every(record => record.oldValue === null && record.attributeNamespace === null) && emptyRecords.length === 0);
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("filter/defaults or old-value separation failed: %q %v", got, err)
	}
}

func TestMutationObserverCharacterDataCapturesActualOldValues(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target">seed</div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const target = document.getElementById('target'), text = target.firstChild;
		const observer = new MutationObserver(() => {});
		observer.observe(target, {characterDataOldValue: true, subtree: true});
		text.data = 'next'; text.nodeValue = 'last';
		const records = observer.takeRecords(); observer.disconnect();
		return String(records.length === 2 && records[0].type === 'characterData' && records[0].target === text &&
			records[0].oldValue === 'seed' && records[1].oldValue === 'next' && text.data === 'last' &&
			records.every(record => record.attributeName === null && record.attributeNamespace === null && record.addedNodes.length === 0 && record.removedNodes.length === 0));
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("character-data mutation became absent or child-list data: %q %v", got, err)
	}
}

func TestMutationObserverDeliveryUsesConstructionOrder(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `
		globalThis.creationTrace = [];
		const first = new MutationObserver(() => creationTrace.push('first'));
		const second = new MutationObserver(() => creationTrace.push('second'));
		const target = document.getElementById('target');
		second.observe(target, {attributes: true}); first.observe(target, {attributes: true});
		target.setAttribute('data-actual', 'value');
	`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), "creationTrace.join(',')"); err != nil || got != "first,second" {
		t.Fatalf("callback order followed observe calls: %q %v", got, err)
	}
}

func TestMutationObserverPublicOwnershipSurvivesReobserveAndReconnect(t *testing.T) {
	c := newCtxFromHTML(t, `<i id="a"></i><b id="b"></b>`, nil)
	if err := c.EvalDiscard(t.Context(), `globalThis.observedA = document.getElementById('a'); globalThis.observedB = document.getElementById('b');`); err != nil {
		t.Fatal(err)
	}
	const exercise = `(() => {
		const observer = new MutationObserver(() => { throw Error('unexpected delivery'); });
		for (let index = 0; index < 50; index++) {
			observer.observe(observedA, {attributes: true, attributeFilter: ['data-first']});
			observer.observe(observedB, {childList: true});
			observer.observe(observedA, {attributes: true, attributeOldValue: true});
			observer.disconnect();
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
		t.Fatalf("public reobserve/reconnect retained handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
	if len(c.observers.by) != 0 || len(c.observers.byTarget) != 0 || c.observers.registrations != 0 || c.observers.optionsBytes != 0 {
		t.Fatal("public reconnect retained registrations/filter state")
	}
}

func TestMutationObserverUsesActualDocumentAndNodeIdentity(t *testing.T) {
	c := newCtxFromHTML(t, `<i id="target"></i>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const target = document.getElementById('target'), observer = new MutationObserver(() => {});
		let rejected = false;
		try { observer.observe({__id: target.__id}, {attributes: true}); }
		catch (error) { rejected = error instanceof TypeError; }
		observer.observe(document, {attributes: true, subtree: true});
		target.setAttribute('data-actual', 'value');
		const records = observer.takeRecords(); observer.disconnect();
		return String(rejected && records.length === 1 && records[0].target === target);
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("forged target accepted or actual Document observation lost: %q %v", got, err)
	}
}

func TestMutationObserverOverlappingTargetsMergeOldValueInterest(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="root"><i id="target" data-match="seed"></i></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const target = document.getElementById('target'), observer = new MutationObserver(() => {});
		observer.observe(target, {attributes: true});
		observer.observe(document.getElementById('root'), {attributes: true, attributeOldValue: true, subtree: true});
		target.setAttribute('data-match', 'next');
		const records = observer.takeRecords(); observer.disconnect();
		return String(records.length === 1 && records[0].oldValue === 'seed');
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("overlapping target old-value interest was lost or duplicated: %q %v", got, err)
	}
}

func TestMutationObserverFilterLimitsRejectBeforeChangingRegistration(t *testing.T) {
	c := newCtxFromHTML(t, `<i id="target" data-match="seed"></i>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const target = document.getElementById('target'), observer = new MutationObserver(() => {});
		observer.observe(target, {attributes: true});
		let rejected = 0, closed = 0, converted = 0;
		function* names() {
			try { for (let index = 0; index < 1025; index++) yield {toString() { converted++; return 'data-match'; }}; }
			finally { closed++; }
		}
		for (const filter of [names(), ['é'.repeat(32769)]]) {
			try { observer.observe(target, {attributeFilter: filter}); }
			catch (error) { if (!String(error).includes('attribute filter')) throw error; rejected++; }
		}
		target.setAttribute('data-match', 'next');
		const records = observer.takeRecords(); observer.disconnect();
		return String(rejected === 2 && closed === 1 && converted === 1024 && records.length === 1 && records[0].attributeName === 'data-match');
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("bounded filter conversion changed ownership or failed to close its iterator: %q %v", got, err)
	}
}

func TestMutationObserverOptionsBudgetRecoversAndCloses(t *testing.T) {
	c := newCtxFromHTML(t, `<i id="target"></i>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const target = document.getElementById('target'), observers = [], name = 'x'.repeat(65536);
		let rejected = false;
		for (let index = 0; index < 80; index++) {
			const observer = new MutationObserver(() => {});
			try { observer.observe(target, {attributeFilter: [name]}); observers.push(observer); }
			catch (error) { if (!String(error).includes('options byte limit exceeded')) throw error; rejected = true; break; }
		}
		for (const observer of observers) observer.disconnect();
		const recovered = new MutationObserver(() => {});
		recovered.observe(target, {attributes: true}); recovered.disconnect();
		return String(rejected && observers.length > 0 && observers.length < 80);
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("aggregate filter budget did not reject/recover: %q %v", got, err)
	}
	if c.observers.optionsBytes != 0 || c.observers.registrations != 0 || len(c.observers.by) != 0 {
		t.Fatal("filter budget retained closed registrations")
	}
	if err := c.EvalDiscard(t.Context(), `new MutationObserver(() => {}).observe(document.getElementById('target'), {attributeFilter: ['data-match']});`); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if c.observers.optionsBytes != 0 || c.observers.registrations != 0 || len(c.observers.byTarget) != 0 {
		t.Fatal("close retained filter ownership")
	}
}

func TestMutationObserverTargetQuotaCountsRegistrationsNotCallbacks(t *testing.T) {
	c := newCtxFromHTML(t, `<i id="target"></i>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const observer = new MutationObserver(() => {}), targets = [];
		for (let index = 0; index < 4096; index++) {
			const target = document.createElement('i'); targets.push(target); observer.observe(target, {attributes: true});
		}
		observer.observe(targets[0], {attributes: true, attributeFilter: ['data-match']});
		let rejected = false;
		try { observer.observe(document.getElementById('target'), {attributes: true}); }
		catch (error) { rejected = String(error).includes('registration limit exceeded'); }
		observer.disconnect(); observer.observe(targets[0], {attributes: true}); observer.disconnect();
		return String(rejected);
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("multi-target registration escaped quota or blocked replacement/recovery: %q %v", got, err)
	}
	if c.observers.registrations != 0 || c.observers.optionsBytes != 0 || len(c.observers.byTarget) != 0 {
		t.Fatal("target quota recovery retained ownership")
	}
}

func TestMutationObserverNativeAdmissionRejectsMalformedMetadataWithoutRetention(t *testing.T) {
	c := newCtxFromHTML(t, `<i id="target"></i>`, nil)
	if err := c.EvalDiscard(t.Context(), `globalThis.actualObserverTarget = document.getElementById('target').__id;`); err != nil {
		t.Fatal(err)
	}
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	if err := c.EvalDiscard(t.Context(), `(() => {
		let rejected = 0, coerced = 0;
		const badLength = new Proxy([], {get(target, key) { if (key === 'length') return {valueOf() { coerced++; throw Error('length coerced'); }}; return target[key]; }});
		const cases = [[-1, {attributes:true}], [1.5, {attributes:true}], [NaN, {attributes:true}], ['1', {attributes:true}],
			[actualObserverTarget, {attributes:true}, 999999], [actualObserverTarget, {attributes:true}, 0, Infinity],
			[actualObserverTarget, {attributes:true}, 0, false], [actualObserverTarget, {attributeFilter: badLength}],
			[actualObserverTarget, {attributeFilter: ['x'.repeat(1024 * 1024)]}]];
		for (const [target, options, owner, order] of cases) {
			try { __observer_register(target, options, () => {}, owner, order === undefined ? 0 : order); }
			catch (error) { rejected++; }
		}
		if (rejected !== cases.length || coerced !== 0) throw Error('malformed native metadata admitted or coerced');
	})()`); err != nil {
		t.Fatal(err)
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("malformed registration retained handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
	if len(c.observers.by) != 0 || c.observers.registrations != 0 || c.observers.optionsBytes != 0 {
		t.Fatal("malformed registration retained authority/options")
	}
}

func TestMutationObserverFilterAndRecordsIgnoreInheritedArraySetters(t *testing.T) {
	c := newCtxFromHTML(t, `<i id="target"></i>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const target = document.getElementById('target'), observer = new MutationObserver(() => {}), filter = ['data-match'];
		let writes = 0, records;
		Object.defineProperty(Object.prototype, '0', {configurable: true, set(value) { writes++; }});
		try {
			observer.observe(target, {attributeFilter: filter}); target.setAttribute('data-match', 'actual');
			records = observer.takeRecords();
		} finally { delete Object.prototype['0']; observer.disconnect(); }
		return String(writes === 0 && records.length === 1 && records[0].target === target && records[0].attributeName === 'data-match');
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("inherited numeric setter intercepted private filters/records: %q %v", got, err)
	}
}

func TestMutationObserverOldValueBytesParticipateInPendingBudget(t *testing.T) {
	c := newCtxFromHTML(t, `<i id="target">seed</i>`, nil)
	err := c.EvalDiscard(t.Context(), `(() => {
		const text = document.getElementById('target').firstChild;
		text.data = 'x'.repeat(4 * 1024 * 1024 + 1);
		const observer = new MutationObserver(() => { throw Error('excess old value delivered'); });
		observer.observe(text, {characterDataOldValue: true});
		let rejected = false;
		try { text.data = 'actual'; } catch (error) { rejected = String(error).includes('pending byte limit exceeded'); }
		const records = observer.takeRecords(); observer.disconnect();
		globalThis.oldValueBudgetResult = String(rejected && records.length === 0 && text.data === 'actual');
	})()`)
	if err == nil || !strings.Contains(err.Error(), "pending byte limit exceeded") {
		t.Fatalf("retained old data escaped the byte bound: %v", err)
	}
	if got, err := c.EvalString(t.Context(), "oldValueBudgetResult"); err != nil || got != "true" {
		t.Fatalf("excess old-value rejection lost recovery: %q %v", got, err)
	}
	if c.observers.pendingBytes != 0 || c.observers.pendingRecords != 0 {
		t.Fatal("rejected old value retained pending ownership")
	}
}
