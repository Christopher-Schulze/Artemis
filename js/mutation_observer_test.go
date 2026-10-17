package js

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestMutationObserverFailureAndRegistrationOrderAreObservable(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	_, err := c.EvalString(t.Context(), `
		globalThis.observerTrace = [];
		for (let index = 0; index < 16; index++) {
			new MutationObserver(() => { observerTrace.push(index); if (index === 0) throw Error('actual-observer-failure'); })
				.observe(document.getElementById('target'), {attributes: true});
		}
		document.getElementById('target').setAttribute('data-trigger', 'changed'); 'triggered';
	`)
	if err == nil || !strings.Contains(err.Error(), "actual-observer-failure") {
		t.Fatalf("observer callback failure became success: %v", err)
	}
	want := ""
	for index := range 16 {
		if index > 0 {
			want += ","
		}
		want += fmt.Sprint(index)
	}
	if got, err := c.EvalString(t.Context(), "observerTrace.join(',')"); err != nil || got != want {
		t.Fatalf("observer delivery lost order, subsequent listeners or recovery: %q %v", got, err)
	}
}

func TestMutationObserverFailureOverflowRemainsObservable(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	_, err := c.EvalString(t.Context(), `
		globalThis.observerTrace = [];
		const target = document.getElementById('target');
		for (let index = 0; index < 20; index++) {
			new MutationObserver(() => {
				observerTrace.push(index);
				throw Error('actual-observer-failure-' + index);
			}).observe(target, {attributes: true});
		}
		target.setAttribute('data-trigger', 'changed'); 'triggered';
	`)
	if err == nil || !strings.Contains(err.Error(), "actual-observer-failure-0") || !strings.Contains(err.Error(), "actual-observer-failure-15") || !strings.Contains(err.Error(), "4 additional mutation observer callback failures omitted") {
		t.Fatalf("bounded observer failure details or omission count were lost: %v", err)
	}
	if strings.Contains(err.Error(), "actual-observer-failure-16") {
		t.Fatalf("observer error details exceeded the reporting bound: %v", err)
	}
	want := ""
	for index := range 20 {
		if index > 0 {
			want += ","
		}
		want += fmt.Sprint(index)
	}
	if got, err := c.EvalString(t.Context(), "observerTrace.join(',')"); err != nil || got != want {
		t.Fatalf("observer failure stopped ordered delivery or recovery: %q %v", got, err)
	}
}

func TestMutationObserverPumpIsBounded(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	_, err := c.EvalString(t.Context(), `
		let callbacks = 0;
		const target = document.getElementById('target');
		new MutationObserver(() => {
			callbacks++; target.textContent = String(callbacks);
			if (callbacks < 5000) target.setAttribute('data-trigger', String(callbacks));
		}).observe(target, {attributes: true});
		target.setAttribute('data-trigger', 'start'); 'triggered';
	`)
	if err == nil || !strings.Contains(err.Error(), "mutation observer callback budget exceeded") {
		t.Fatalf("self-scheduling observers escaped the pump budget: %v", err)
	}
	target, selectErr := c.doc.QuerySelector("#target")
	if selectErr != nil || target == nil {
		t.Fatalf("actual mutated target disappeared: %v", selectErr)
	}
	if got := target.Text(); got != "4096" {
		t.Fatalf("observer pump performed work past its declared bound: %q", got)
	}
	c.Close()
	if len(c.observers.by) != 0 || len(c.observers.byTarget) != 0 {
		t.Fatal("closed context retained observer callbacks")
	}
}

func TestMutationObserverChildListAdd(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var trace = '';
		const obs = new MutationObserver((records) => {
			for (const r of records) trace += r.type + '+' + (r.addedNodes.length) + ';';
		});
		obs.observe(document.getElementById('d'), {childList: true});
		const li = document.createElement('p');
		li.textContent = 'x';
		document.getElementById('d').appendChild(li);
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `trace`)
	if !strings.Contains(v.String(), "childList+1;") {
		t.Errorf("trace = %q, want childList+1;", v.String())
	}
}

func TestMutationObserverAttributesChange(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		new MutationObserver((records) => {
			for (const r of records) captured += r.type + ':' + r.attributeName + ';';
		}).observe(document.getElementById('d'), {attributes: true});
		document.getElementById('d').setAttribute('class', 'active');
		document.getElementById('d').setAttribute('data-x', '1');
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if !strings.Contains(v.String(), "attributes:class;") || !strings.Contains(v.String(), "attributes:data-x;") {
		t.Errorf("captured = %q", v.String())
	}
}

func TestMutationObserverSubtree(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="root"><span id="inner"></span></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var fired = 0;
		new MutationObserver(() => { fired++; }).observe(document.getElementById('root'), {childList: true, subtree: true});
		const t = document.createTextNode('hi');
		document.getElementById('inner').appendChild(t);
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `fired`)
	if v.Int64() < 1 {
		t.Errorf("subtree observer should have fired at least once: fired=%d", v.Int64())
	}
}

func TestMutationObserverDisconnect(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var fired = 0;
		const obs = new MutationObserver(() => { fired++; });
		obs.observe(document.getElementById('d'), {attributes: true});
		document.getElementById('d').setAttribute('a', '1');
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if _, err := c.Eval(context.Background(), `obs.disconnect();`); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if _, err := c.Eval(context.Background(), `document.getElementById('d').setAttribute('b', '2');`); err != nil {
		t.Fatalf("post-disconnect mutate: %v", err)
	}
	v, _ := c.Eval(context.Background(), `fired`)
	if v.Int64() != 1 {
		t.Errorf("fired = %d, want 1 (only first mutation should have fired)", v.Int64())
	}
}

func TestMutationObserverInnerHTMLFires(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d"><p>old</p></div></body></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var fired = 0;
		new MutationObserver(() => { fired++; }).observe(document.getElementById('d'), {childList: true});
		document.getElementById('d').innerHTML = '<span>new</span>';
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `fired`)
	if v.Int64() < 1 {
		t.Errorf("innerHTML setter did not fire MutationObserver")
	}
}

func TestMutationObserverTakeRecordsPreservesCapturedFields(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"><span id="old"></span></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const target = document.getElementById('target');
		const old = document.getElementById('old');
		const added = document.createElement('p');
		const observer = new MutationObserver(() => { throw Error('drained records delivered twice'); });
		observer.observe(target, {childList: true, attributes: true});
		target.appendChild(added);
		target.removeChild(old);
		target.setAttribute('data-actual', 'value');
		const records = observer.takeRecords();
		observer.disconnect();
		return String(records.length === 3 && records.every(r => r.target === target) &&
			records[0].type === 'childList' && records[0].addedNodes[0] === added && records[0].removedNodes.length === 0 &&
			records[1].type === 'childList' && records[1].removedNodes[0] === old && records[1].addedNodes.length === 0 &&
			records[2].type === 'attributes' && records[2].attributeName === 'data-actual' && observer.takeRecords().length === 0);
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("takeRecords lost actual captured fields or redelivered drained records: %q %v", got, err)
	}
}

func TestMutationObserverTransportIgnoresInheritedSetters(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const target = document.getElementById('target');
		const id = __observer_register(target.__id, {attributes: true}, () => {});
		target.setAttribute('data-actual', 'value');
		let writes = 0;
		Object.defineProperty(Object.prototype, 'type', {configurable: true, set() { writes++; }});
		let records;
		try { records = __observer_take(id); }
		finally { delete Object.prototype.type; __observer_disconnect(id); }
		return String(writes === 0 && records.length === 1 && records[0].type === 'attributes' &&
			records[0].targetId === target.__id && records[0].attributeName === 'data-actual');
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("page prototype intercepted native mutation transport: %q %v", got, err)
	}
}

func TestMutationObserverCallbacksRetireNativeHandles(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `globalThis.observedTargetID = document.getElementById('target').__id;`); err != nil {
		t.Fatal(err)
	}
	const exercise = `(() => {
		for (let index = 0; index < 50; index++) {
			const id = __observer_register(observedTargetID, {attributes: true}, () => {});
			if (__observer_take(id).length !== 0) throw Error('unexpected record');
			__observer_disconnect(id);
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
		t.Fatalf("mutation callbacks retained handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
	if len(c.observers.by) != 0 || len(c.observers.byTarget) != 0 {
		t.Fatal("disconnect retained native registrations or target indexes")
	}
}

func TestMutationObserverRecordConstructionRetiresHandles(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"><span></span></div>`, nil)
	for _, test := range []struct {
		name    string
		record  mutationRecord
		invalid bool
	}{
		{"complete record", mutationRecord{Type: MutationChildList, TargetID: 1, AddedIDs: []uint32{2, 3}, RemovedIDs: []uint32{4}}, false},
		{"invalid target", mutationRecord{Type: MutationChildList, TargetID: math.MaxUint32}, true},
		{"invalid previous sibling", mutationRecord{Type: MutationChildList, TargetID: 1, PreviousSiblingID: math.MaxUint32}, true},
		{"invalid next sibling", mutationRecord{Type: MutationChildList, TargetID: 1, NextSiblingID: math.MaxUint32}, true},
		{"invalid added node", mutationRecord{Type: MutationChildList, TargetID: 1, AddedIDs: []uint32{2, math.MaxUint32}}, true},
		{"invalid removed node", mutationRecord{Type: MutationChildList, TargetID: 1, AddedIDs: []uint32{2}, RemovedIDs: []uint32{math.MaxUint32}}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
			for range 20 {
				value, err := mutationsToJSArray(c.v8ctx, []mutationRecord{test.record})
				if (err != nil) != test.invalid {
					t.Fatalf("record error=%v invalid=%t", err, test.invalid)
				}
				if value != nil {
					value.Release()
				}
			}
			if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
				t.Fatalf("record construction retained native handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
			}
		})
	}
}

func TestMutationObserverFailedTakePreservesPendingRecords(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `globalThis.observedTargetID = document.getElementById('target').__id;
		globalThis.actualObserverID = __observer_register(observedTargetID, {attributes: true}, () => {});`); err != nil {
		t.Fatal(err)
	}
	var registered *observer
	for _, owner := range c.observers.by {
		registered = owner
	}
	if registered == nil {
		t.Fatal("actual registration missing")
	}
	var targetID uint32
	for id := range registered.targets {
		targetID = id
	}
	if err := c.observers.recordMutation(c, mutationRecord{Type: MutationAttributes, TargetID: targetID, AttributeName: "data-kept"}, c.nodes.Get(targetID)); err != nil {
		t.Fatal(err)
	}
	// Corrupt only the native publication ID after real queue admission.
	registered.pending[0].TargetID = math.MaxUint32
	got, err := c.EvalString(t.Context(), `(() => {
		try { __observer_take(actualObserverID); return 'silent success'; }
		catch (error) { return String(error); }
	})()`)
	if err == nil || !strings.Contains(err.Error(), "mutation observer target exceeds native bound") {
		// Eval also pumps the still-pending record and must expose that failure.
		t.Fatalf("failed construction became successful delivery: %q %v", got, err)
	}
	if len(registered.pending) != 1 || registered.pending[0].AttributeName != "data-kept" || c.observers.pendingRecords != 1 || c.observers.pendingBytes <= 0 {
		t.Fatal("failed construction consumed the pending record")
	}
	target, selectErr := c.doc.QuerySelector("#target")
	if selectErr != nil || target == nil {
		t.Fatalf("target: %v", selectErr)
	}
	registered.pending[0].TargetID = c.nodes.Handle(target)
	got, err = c.EvalString(t.Context(), `(() => {
		const records = __observer_take(actualObserverID);
		__observer_disconnect(actualObserverID);
		return String(records.length === 1 && records[0].attributeName === 'data-kept');
	})()`)
	if err != nil || got != "true" || len(registered.pending) != 0 || c.observers.pendingRecords != 0 || c.observers.pendingBytes != 0 {
		t.Fatalf("record retry lost state or failed to recover: %q %v", got, err)
	}
}

func TestMutationObserverDeliveredRecordsRetireNativeHandles(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `globalThis.deliveredRecords = 0;
		__observer_register(document.getElementById('target').__id, {attributes: true}, records => {
			if (records.length !== 1 || records[0].attributeName !== 'data-actual') throw Error('record content lost');
			deliveredRecords++;
		});`); err != nil {
		t.Fatal(err)
	}
	target, err := c.doc.QuerySelector("#target")
	if err != nil || target == nil {
		t.Fatalf("target: %v", err)
	}
	record := mutationRecord{Type: MutationAttributes, TargetID: c.nodes.Handle(target), AttributeName: "data-actual"}
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	for range 50 {
		if err := c.observers.recordMutation(c, record, target); err != nil {
			t.Fatal(err)
		}
		if err := c.EvalDiscard(t.Context(), "undefined"); err != nil {
			t.Fatal(err)
		}
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("delivery retained native handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
	if got, err := c.EvalString(t.Context(), "String(deliveredRecords)"); err != nil || got != "50" {
		t.Fatalf("actual delivery count: %q %v", got, err)
	}
}

func TestMutationObserverOptionErrorsDoNotRetainCallbacks(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `globalThis.observedTargetID = document.getElementById('target').__id;`); err != nil {
		t.Fatal(err)
	}
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	if err := c.EvalDiscard(t.Context(), `(() => {
		for (const option of ['childList', 'attributes', 'subtree']) {
			for (let index = 0; index < 20; index++) {
				const cause = 'actual-option-' + option;
				const options = Object.defineProperty({}, option, {get() { throw Error(cause); }});
				let rejected = false;
				try { __observer_register(observedTargetID, options, () => {}); }
				catch (error) { if (!String(error).includes(cause)) throw error; rejected = true; }
				if (!rejected) throw Error('option failure discarded');
			}
		}
	})()`); err != nil {
		t.Fatal(err)
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("failed registration retained handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
	if len(c.observers.by) != 0 || len(c.observers.byTarget) != 0 {
		t.Fatal("throwing options admitted an observer")
	}
}

func TestMutationObserverRegistrationAdmissionIsBoundedAndRecoverable(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `globalThis.observedTargetID = document.getElementById('target').__id;`); err != nil {
		t.Fatal(err)
	}
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	source := fmt.Sprintf(`(() => {
		const ids = [];
		for (let index = 0; index < %d; index++) ids.push(__observer_register(observedTargetID, {attributes: true}, () => {}));
		let rejected = false;
		try { __observer_register(observedTargetID, {attributes: true}, () => {}); }
		catch (error) { if (!String(error).includes('registration limit exceeded')) throw error; rejected = true; }
		if (!rejected) throw Error('registration escaped quota');
		__observer_disconnect(ids.pop());
		const recovered = __observer_register(observedTargetID, {attributes: true}, () => {});
		if (!Number.isInteger(recovered) || recovered <= ids[ids.length - 1]) throw Error('identity reused or invalid');
		__observer_disconnect(recovered);
		for (const id of ids) __observer_disconnect(id);
	})()`, maxMutationRegistrationsPerContext)
	if err := c.EvalDiscard(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("quota rejection/recovery leaked handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
	if len(c.observers.by) != 0 || len(c.observers.byTarget) != 0 {
		t.Fatal("quota recovery retained registrations")
	}
	c.observers.nextID = math.MaxInt32
	got, err := c.EvalString(t.Context(), `(() => {
		try { __observer_register(observedTargetID, {attributes: true}, () => {}); return 'silent success'; }
		catch (error) { return String(error); }
	})()`)
	if err != nil || !strings.Contains(got, "identity exhausted") || c.observers.nextID != math.MaxInt32 || len(c.observers.by) != 0 {
		t.Fatalf("exhausted identity wrapped or admitted a callback: %q %v", got, err)
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("identity rejection leaked handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
}
