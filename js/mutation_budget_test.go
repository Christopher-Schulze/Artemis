package js

import (
	"fmt"
	"strings"
	"testing"
)

func TestMutationObserverPendingCountAdmissionIsAtomicAndRecoverable(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="root"><span id="target"></span></div>`, nil)
	err := c.EvalDiscard(t.Context(), `(() => {
		const target = document.getElementById('target');
		const first = new MutationObserver(() => { throw Error('records unexpectedly delivered'); });
		const second = new MutationObserver(() => { throw Error('records unexpectedly delivered'); });
		first.observe(target, {attributes: true});
		second.observe(document.getElementById('root'), {attributes: true, subtree: true});
		for (let index = 0; index < 2048; index++) target.setAttribute('data-actual', String(index));
		let rejected = false;
		try { target.setAttribute('data-overflow', 'rejected'); }
		catch (error) { if (!String(error).includes('pending record limit exceeded')) throw error; rejected = true; }
		const firstRecords = first.takeRecords();
		const secondRecords = second.takeRecords();
		target.setAttribute('data-recovered', 'actual');
		const firstRecovery = first.takeRecords();
		const secondRecovery = second.takeRecords();
		first.disconnect(); second.disconnect();
		globalThis.pendingQuotaResult = String(rejected && firstRecords.length === 2048 && secondRecords.length === 2048 &&
			firstRecords.every(record => record.attributeName === 'data-actual') &&
			secondRecords.every(record => record.attributeName === 'data-actual') &&
			firstRecovery.length === 1 && firstRecovery[0].attributeName === 'data-recovered' &&
			secondRecovery.length === 1 && secondRecovery[0].attributeName === 'data-recovered');
	})()`)
	if err == nil || !strings.Contains(err.Error(), "pending record limit exceeded") {
		t.Fatalf("caught observer overflow acknowledged partial success: %v", err)
	}
	if got, err := c.EvalString(t.Context(), "pendingQuotaResult"); err != nil || got != "true" {
		t.Fatalf("global queue quota lost atomic fanout, retained records or capacity recovery: %q %v", got, err)
	}
}

func TestMutationObserverPendingByteAdmissionIsAtomicAndRecoverable(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	err := c.EvalDiscard(t.Context(), `(() => {
		const target = document.getElementById('target');
		const first = new MutationObserver(() => {});
		const second = new MutationObserver(() => {});
		first.observe(target, {attributes: true}); second.observe(target, {attributes: true});
		const name = 'a'.repeat(1024 * 1024);
		target.setAttribute(name, 'first');
		let rejected = false;
		try { target.setAttribute(name, 'overflow'); }
		catch (error) { if (!String(error).includes('pending byte limit exceeded')) throw error; rejected = true; }
		const firstRecords = first.takeRecords();
		const secondRecords = second.takeRecords();
		first.disconnect(); second.disconnect();
		const recovered = new MutationObserver(() => {});
		recovered.observe(target, {attributes: true});
		target.setAttribute('data-recovered', 'actual');
		const recoveredRecords = recovered.takeRecords(); recovered.disconnect();
		globalThis.pendingByteResult = String(rejected && firstRecords.length === 1 && secondRecords.length === 1 &&
			firstRecords[0].attributeName === name && secondRecords[0].attributeName === name &&
			recoveredRecords.length === 1 && recoveredRecords[0].attributeName === 'data-recovered');
	})()`)
	if err == nil || !strings.Contains(err.Error(), "pending byte limit exceeded") {
		t.Fatalf("caught observer byte overflow acknowledged partial success: %v", err)
	}
	if got, err := c.EvalString(t.Context(), "pendingByteResult"); err != nil || got != "true" {
		t.Fatalf("byte quota lost atomic fanout, captured data or recovery: %q %v", got, err)
	}
}

func TestMutationObserverDeliveryReleasesQuotaBeforeCallback(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `
		globalThis.deliverySizes = [];
		const target = document.getElementById('target');
		const observer = new MutationObserver(records => {
			deliverySizes.push(records.length);
			if (deliverySizes.length === 1) target.setAttribute('data-followup', 'actual');
			else observer.disconnect();
		});
		observer.observe(target, {attributes: true});
		for (let index = 0; index < 4096; index++) target.setAttribute('data-actual', String(index));
	`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), "deliverySizes.join(',')"); err != nil || got != "4096,1" {
		t.Fatalf("delivery did not release quota before reentrant mutations: %q %v", got, err)
	}
	if c.observers.pendingRecords != 0 || c.observers.pendingBytes != 0 || len(c.observers.by) != 0 {
		t.Fatal("delivered/disconnected observers retained pending ownership")
	}
}

func TestMutationObserverPendingOwnershipCountsRetainedBackingAndCloses(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="target"></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `__observer_register(document.getElementById('target').__id, {attributes: true, childList: true}, () => {});`); err != nil {
		t.Fatal(err)
	}
	target, err := c.doc.QuerySelector("#target")
	if err != nil || target == nil {
		t.Fatalf("target: %v", err)
	}
	record := mutationRecord{Type: MutationChildList, TargetID: c.nodes.Handle(target), AddedIDs: make([]uint32, 1, maxMutationPendingBytes/4+1)}
	if err := c.observers.recordMutation(c, record, target); err == nil || !strings.Contains(err.Error(), "pending byte limit exceeded") {
		t.Fatalf("excess retained slice backing escaped quota: %v", err)
	}
	if c.observers.pendingRecords != 0 || c.observers.pendingBytes != 0 || len(c.observers.take()) != 0 {
		t.Fatal("rejected backing store consumed pending ownership")
	}
	record = mutationRecord{Type: MutationAttributes, TargetID: record.TargetID, AttributeName: "data-actual"}
	if err := c.observers.recordMutation(c, record, target); err != nil {
		t.Fatal(err)
	}
	if c.observers.pendingRecords != 1 || c.observers.pendingBytes <= len(record.AttributeName) {
		t.Fatal("admitted record missing retained metadata accounting")
	}
	c.Close()
	if c.observers.pendingRecords != 0 || c.observers.pendingBytes != 0 || len(c.observers.by) != 0 || len(c.observers.byTarget) != 0 {
		t.Fatal("close retained pending records or bytes")
	}
}

func TestMutationObserverRejectedDOMWritesPreserveActualTree(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="source"><p id="child"></p><i id="after"></i></div><div id="target"><b id="ref"></b></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const source = document.getElementById('source'), child = document.getElementById('child');
		const target = document.getElementById('target'), reference = document.getElementById('ref');
		const observer = new MutationObserver(() => { throw Error('rejected mutation delivered'); });
		observer.observe(document.body, {childList: true, subtree: true});
		let rejected = 0;
		for (const operation of [() => target.removeChild(child), () => source.insertBefore(child, reference),
			() => child.appendChild(source), () => child.insertBefore(source, null)]) {
			try { operation(); } catch (error) { rejected++; }
		}
		const records = observer.takeRecords(); observer.disconnect();
		return String(rejected === 4 && records.length === 0 && child.parentNode === source &&
			source.firstChild === child && child.nextSibling === document.getElementById('after') && reference.parentNode === target);
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("failed DOM write acknowledged success, damaged the tree or emitted a record: %q %v", got, err)
	}
}

func TestMutationObserverEveryDOMWriterReportsPendingOverflow(t *testing.T) {
	for _, writer := range []struct{ name, source string }{
		{"inner HTML", `writerTarget.innerHTML = '<strong>actual</strong>'`},
		{"text content", `writerTarget.textContent = 'actual'`},
		{"set attribute", `writerTarget.setAttribute('data-actual', 'value')`},
		{"remove attribute", `writerTarget.removeAttribute('data-remove')`},
		{"append child", `writerTarget.appendChild(document.createElement('strong'))`},
		{"remove child", `writerTarget.removeChild(writerTarget.firstChild)`},
		{"insert before", `writerTarget.insertBefore(document.createElement('strong'), writerTarget.firstChild)`},
		{"inline CSS", `writerTarget.style.setProperty('color', 'red')`},
	} {
		t.Run(writer.name, func(t *testing.T) {
			c := newCtxFromHTML(t, `<div id="target" data-remove="kept"><i></i></div>`, nil)
			if err := c.EvalDiscard(t.Context(), `globalThis.writerTarget = document.getElementById('target');
				globalThis.writerObserverID = __observer_register(writerTarget.__id, {attributes: true, childList: true}, () => {});`); err != nil {
				t.Fatal(err)
			}
			target, err := c.doc.QuerySelector("#target")
			if err != nil || target == nil {
				t.Fatalf("target: %v", err)
			}
			for range maxMutationPendingRecords {
				if err := c.observers.recordMutation(c, mutationRecord{Type: MutationAttributes, TargetID: c.nodes.Handle(target), AttributeName: "data-seeded"}, target); err != nil {
					t.Fatal(err)
				}
			}
			err = c.EvalDiscard(t.Context(), fmt.Sprintf(`globalThis.writerRejected = false; globalThis.writerReturned = false;
				try { %s; writerReturned = true; }
				catch (error) { writerRejected = String(error).includes('pending record limit exceeded'); }
				__observer_disconnect(writerObserverID);`, writer.source))
			if err == nil || !strings.Contains(err.Error(), "pending record limit exceeded") {
				t.Fatalf("overflow acknowledged success: %v", err)
			}
			if got, err := c.EvalString(t.Context(), "String(writerRejected) + ':' + String(writerReturned)"); err != nil || got != "true:false" {
				t.Fatalf("DOM writer swallowed native queue failure: %q %v", got, err)
			}
			if c.observers.pendingRecords != 0 || c.observers.pendingBytes != 0 {
				t.Fatal("disconnect failed to release quota")
			}
		})
	}
}
