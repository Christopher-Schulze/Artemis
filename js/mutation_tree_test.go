package js

import (
	"math"
	"strings"
	"testing"
)

func TestMutationObserverTreeRecordsPreserveActualSiblingPositions(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="parent"><i id="a"></i><b id="b"></b></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const parent = document.getElementById('parent'), a = document.getElementById('a'), b = document.getElementById('b');
		const observer = new MutationObserver(() => {}), inserted = document.createElement('u'), appended = document.createElement('em');
		observer.observe(parent, {childList: true});
		parent.insertBefore(inserted, b); parent.removeChild(a); parent.appendChild(appended);
		const records = observer.takeRecords(); observer.disconnect();
		return String(records.length === 3 && records[0].addedNodes[0] === inserted && records[0].previousSibling === a && records[0].nextSibling === b &&
			records[1].removedNodes[0] === a && records[1].previousSibling === null && records[1].nextSibling === inserted &&
			records[2].addedNodes[0] === appended && records[2].previousSibling === b && records[2].nextSibling === null);
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("mutation record lost captured sibling positions: %q %v", got, err)
	}
}

func TestMutationObserverMovesPreserveRemovalAndAdditionOrder(t *testing.T) {
	for _, method := range []string{"appendChild", "insertBefore"} {
		t.Run(method, func(t *testing.T) {
			c := newCtxFromHTML(t, `<main id="root"><div id="source"><i id="left"></i><b id="child"></b><u id="right"></u></div><div id="destination"><em id="existing"></em></div></main>`, nil)
			if err := c.EvalDiscard(t.Context(), "globalThis.moveMethod = '"+method+"';"); err != nil {
				t.Fatal(err)
			}
			got, err := c.EvalString(t.Context(), `(() => {
				const source = document.getElementById('source'), destination = document.getElementById('destination');
				const child = document.getElementById('child'), existing = document.getElementById('existing');
				const observer = new MutationObserver(() => {}); observer.observe(document.getElementById('root'), {childList:true, subtree:true});
				if (moveMethod === 'appendChild') destination.appendChild(child); else destination.insertBefore(child, existing);
				const records = observer.takeRecords(); observer.disconnect();
				return String(records.length === 2 && records[0].target === source && records[0].removedNodes[0] === child &&
					records[0].previousSibling === document.getElementById('left') && records[0].nextSibling === document.getElementById('right') &&
					records[1].target === destination && records[1].addedNodes[0] === child && child.parentNode === destination &&
					(moveMethod === 'appendChild' ? records[1].previousSibling === existing && records[1].nextSibling === null : records[1].previousSibling === null && records[1].nextSibling === existing));
			})()`)
			if err != nil || got != "true" {
				t.Fatalf("move omitted/reordered original removal or destination addition: %q %v", got, err)
			}
		})
	}
}

func TestMutationObserverRemovedSubtreeSurvivesTakeUntilNotification(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="root"><section id="branch"><i id="leaf"></i></section></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		globalThis.removedBranch = document.getElementById('branch'); globalThis.removedLeaf = document.getElementById('leaf');
		globalThis.removedObserver = new MutationObserver(() => { throw Error('drained record redelivered'); });
		const root = document.getElementById('root'); removedObserver.observe(root, {attributes: true, subtree:true});
		root.removeChild(removedBranch); removedLeaf.setAttribute('data-first', 'actual');
		const first = removedObserver.takeRecords(); removedLeaf.setAttribute('data-second', 'actual');
		const second = removedObserver.takeRecords();
		return String(first.length === 1 && first[0].target === removedLeaf && first[0].attributeName === 'data-first' &&
			second.length === 1 && second[0].attributeName === 'data-second');
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("removed subtree tracking ended before notification: %q %v", got, err)
	}
	got, err = c.EvalString(t.Context(), `removedLeaf.setAttribute('data-after', 'actual'); String(removedObserver.takeRecords().length)`)
	if err != nil || got != "0" {
		t.Fatalf("removed subtree tracking outlived notification: %q %v", got, err)
	}
}

func TestMutationObserverNestedRemovalAndReobserveRetireSourceTracking(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="root"><section id="branch"><i id="leaf"></i></section></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const root = document.getElementById('root'), branch = document.getElementById('branch'), leaf = document.getElementById('leaf');
		const observer = new MutationObserver(() => {}); observer.observe(root, {attributes:true, subtree:true});
		root.removeChild(branch); branch.removeChild(leaf); leaf.setAttribute('data-kept', 'actual');
		const kept = observer.takeRecords(); observer.observe(root, {attributes:true, subtree:true});
		leaf.setAttribute('data-after', 'ignored'); const after = observer.takeRecords(); observer.disconnect();
		return String(kept.length === 1 && kept[0].target === leaf && kept[0].attributeName === 'data-kept' && after.length === 0);
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("nested detach lost inherited interest or reobserve retained it: %q %v", got, err)
	}
}

func TestMutationObserverRetiresRemovedSubtreeBeforeItsCallback(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="root"><section id="branch"></section></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `
		globalThis.removalCallbacks = 0;
		const root = document.getElementById('root'), branch = document.getElementById('branch');
		const observer = new MutationObserver(records => {
			removalCallbacks++; if (records.length !== 1 || records[0].removedNodes[0] !== branch) throw Error('removal record lost');
			branch.setAttribute('data-after-notification', 'ignored');
		});
		observer.observe(root, {childList:true, attributes:true, subtree:true}); root.removeChild(branch);
	`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), "String(removalCallbacks)"); err != nil || got != "1" {
		t.Fatalf("transient subtree survived callback entry: %q %v", got, err)
	}
}

func TestMutationObserverForeignSelfReferenceDoesNotMoveNode(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="source"><i id="child"></i></div><div id="destination"></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const source = document.getElementById('source'), child = document.getElementById('child'), destination = document.getElementById('destination');
		const observer = new MutationObserver(() => {}); observer.observe(document.body, {childList:true, subtree:true});
		let rejected = false;
		try { destination.insertBefore(child, child); } catch (error) { rejected = true; }
		const records = observer.takeRecords(); observer.disconnect();
		return String(rejected && child.parentNode === source && source.firstChild === child && destination.childNodes.length === 0 && records.length === 0);
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("foreign self-reference moved a node before reference validation: %q %v", got, err)
	}
}

func TestMutationObserverReplacementRetainsRemovedSubtrees(t *testing.T) {
	for _, property := range []string{"innerHTML", "textContent"} {
		t.Run(property, func(t *testing.T) {
			c := newCtxFromHTML(t, `<div id="root"><section id="branch"><i id="leaf"></i></section></div>`, nil)
			if err := c.EvalDiscard(t.Context(), "globalThis.replaceProperty = '"+property+"';"); err != nil {
				t.Fatal(err)
			}
			got, err := c.EvalString(t.Context(), `(() => {
				const root = document.getElementById('root'), branch = document.getElementById('branch'), leaf = document.getElementById('leaf');
				const observer = new MutationObserver(() => {}); observer.observe(root, {childList:true, attributes:true, subtree:true});
				root[replaceProperty] = 'replacement'; leaf.setAttribute('data-detached', 'actual');
				const records = observer.takeRecords(); observer.disconnect();
				return String(records.length === 2 && records[0].target === root && records[0].removedNodes[0] === branch &&
					records[0].previousSibling === null && records[0].nextSibling === null && records[1].target === leaf && records[1].attributeName === 'data-detached');
			})()`)
			if err != nil || got != "true" {
				t.Fatalf("replacement lost actual removed subtree or subsequent mutation: %q %v", got, err)
			}
		})
	}
}

func TestMutationObserverTransientCleanupWithoutRecordsIsBounded(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="root"><section id="branch"></section></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `
		globalThis.noRecordCallbacks = 0;
		const root = document.getElementById('root'), branch = document.getElementById('branch');
		globalThis.noRecordObserver = new MutationObserver(() => noRecordCallbacks++);
		noRecordObserver.observe(root, {attributes:true, subtree:true}); root.removeChild(branch);
	`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), "String(noRecordCallbacks)"); err != nil || got != "0" {
		t.Fatalf("transient-only notification invoked callback: %q %v", got, err)
	}
	if len(c.observers.transientTargets) != 0 || c.observers.registrations != 1 || c.observers.pendingRecords != 0 {
		t.Fatal("recordless notification retained transient ownership")
	}
	c.Close()
	if c.observers.registrations != 0 || c.observers.optionsBytes != 0 || len(c.observers.transientTargets) != 0 {
		t.Fatal("close retained transient state")
	}
}

func TestMutationObserverTransientLifetimeIsPerCallbackOwner(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="root"><section id="branch"></section></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `
		globalThis.transientTrace = [];
		const root = document.getElementById('root'), branch = document.getElementById('branch');
		const first = new MutationObserver(records => { transientTrace.push('first:' + records.length); branch.setAttribute('data-from-first', 'actual'); });
		const second = new MutationObserver(records => { transientTrace.push('second:' + records.length); first.disconnect(); second.disconnect(); });
		first.observe(root, {attributes:true, childList:true, subtree:true}); second.observe(root, {attributes:true, childList:true, subtree:true});
		root.removeChild(branch);
	`); err != nil {
		t.Fatal(err)
	}
	if got, err := c.EvalString(t.Context(), "transientTrace.join(',')"); err != nil || got != "first:1,second:2" {
		t.Fatalf("one callback retired another owner's transient interest: %q %v", got, err)
	}
	if c.observers.registrations != 0 || c.observers.optionsBytes != 0 || len(c.observers.transientTargets) != 0 {
		t.Fatal("callback disconnect retained transient ownership")
	}
}

func TestMutationObserverMoveAdmissionDoesNotPartiallyQueue(t *testing.T) {
	c := newCtxFromHTML(t, `<main id="root"><div id="source"><i id="child"></i></div><div id="destination"></div></main>`, nil)
	if err := c.EvalDiscard(t.Context(), `globalThis.moveObserver = new MutationObserver(() => {});
		moveObserver.observe(document.getElementById('root'), {attributes:true, childList:true, subtree:true});`); err != nil {
		t.Fatal(err)
	}
	root, err := c.doc.QuerySelector("#root")
	if err != nil || root == nil {
		t.Fatalf("root: %v", err)
	}
	for index := 0; index < maxMutationPendingRecords-1; index++ {
		if err := c.observers.recordMutation(c, mutationRecord{Type: MutationAttributes, TargetID: c.nodes.Handle(root), AttributeName: "data-seeded"}, root); err != nil {
			t.Fatal(err)
		}
	}
	err = c.EvalDiscard(t.Context(), `(() => {
		const source = document.getElementById('source'), child = document.getElementById('child'), destination = document.getElementById('destination');
		let rejected = false;
		try { destination.appendChild(child); } catch (error) { rejected = String(error).includes('pending record limit exceeded'); }
		const records = moveObserver.takeRecords(); moveObserver.disconnect();
		globalThis.moveQuotaResult = String(rejected && records.length === 4095 && records.every(record => record.type === 'attributes') && child.parentNode === destination && source.childNodes.length === 0);
	})()`)
	if err == nil || !strings.Contains(err.Error(), "pending record limit exceeded") {
		t.Fatalf("move overflow acknowledged partial success: %v", err)
	}
	if got, err := c.EvalString(t.Context(), "moveQuotaResult"); err != nil || got != "true" {
		t.Fatalf("move admission committed only one event or lost prior records: %q %v", got, err)
	}
	if c.observers.registrations != 0 || c.observers.optionsBytes != 0 || c.observers.pendingRecords != 0 || len(c.observers.transientTargets) != 0 {
		t.Fatal("failed move retained partial ownership")
	}
}

func TestMutationObserverReobserveRetiresOnlyMatchingTransientSource(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="root"><i id="branch" data-match="seed"></i></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const root = document.getElementById('root'), branch = document.getElementById('branch'), observer = new MutationObserver(() => {});
		observer.observe(root, {attributeOldValue:true, subtree:true}); observer.observe(branch, {attributes:true});
		root.removeChild(branch); branch.setAttribute('data-match', 'next');
		observer.observe(root, {attributes:true, subtree:true}); branch.setAttribute('data-match', 'last');
		const records = observer.takeRecords(); observer.disconnect();
		return String(records.length === 2 && records[0].oldValue === 'seed' && records[1].oldValue === null && records.every(record => record.target === branch));
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("reobserve discarded permanent interest, queued records or retained stale source options: %q %v", got, err)
	}
}

func TestMutationObserverRepeatedDetachDeduplicatesAndCloseReleases(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="root"><i id="branch"></i></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `new MutationObserver(() => {}).observe(document.getElementById('root'), {attributes:true, subtree:true});`); err != nil {
		t.Fatal(err)
	}
	root, err := c.doc.QuerySelector("#root")
	if err != nil || root == nil {
		t.Fatalf("root: %v", err)
	}
	branch := root.FirstChild()
	if branch == nil {
		t.Fatal("actual child missing")
	}
	baselineBytes := c.observers.optionsBytes
	for range 3 {
		if err := c.removeObservedChild(root, branch); err != nil {
			t.Fatal(err)
		}
		if err := c.insertObservedChild(root, branch, nil); err != nil {
			t.Fatal(err)
		}
		if c.observers.registrations != 2 || len(c.observers.transientTargets) != 1 {
			t.Fatal("repeated detach duplicated inherited registration")
		}
	}
	if err := c.EvalDiscard(t.Context(), "undefined"); err != nil {
		t.Fatal(err)
	}
	if c.observers.registrations != 1 || c.observers.optionsBytes != baselineBytes || len(c.observers.transientTargets) != 0 {
		t.Fatal("notification retained inherited ownership")
	}
	if err := c.removeObservedChild(root, branch); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if c.observers.registrations != 0 || c.observers.optionsBytes != 0 || len(c.observers.transientTargets) != 0 || len(c.observers.by) != 0 {
		t.Fatal("close retained active transient state")
	}
}

func TestMutationObserverTransientLimitsRejectWithoutPartialAdmission(t *testing.T) {
	for _, kind := range []string{"registrations", "options"} {
		t.Run(kind, func(t *testing.T) {
			c := newCtxFromHTML(t, `<div id="root"><i id="branch"></i></div>`, nil)
			if err := c.EvalDiscard(t.Context(), "globalThis.transientLimitKind = '"+kind+"';"); err != nil {
				t.Fatal(err)
			}
			err := c.EvalDiscard(t.Context(), `(() => {
				const root = document.getElementById('root'), branch = document.getElementById('branch'), observers = [];
				const count = transientLimitKind === 'registrations' ? 4096 : 63;
				const filter = ['x'.repeat(65536)];
				for (let index = 0; index < count; index++) {
					const observer = new MutationObserver(() => { throw Error('failed removal delivered'); });
					observer.observe(root, transientLimitKind === 'registrations' ? {attributes:true, subtree:index === 0} : {attributeFilter:filter, subtree:true});
					observers.push(observer);
				}
				let rejected = false;
				try { root.removeChild(branch); } catch (error) { rejected = String(error).includes('transient'); }
				let records = 0;
				for (const observer of observers) { records += observer.takeRecords().length; observer.disconnect(); }
				globalThis.transientQuotaResult = String(rejected && records === 0 && branch.parentNode === null);
			})()`)
			if err == nil || !strings.Contains(err.Error(), "mutation observer transient") {
				t.Fatalf("transient quota did not fail its owning operation: %v", err)
			}
			if got, err := c.EvalString(t.Context(), "transientQuotaResult"); err != nil || got != "true" {
				t.Fatalf("transient failure queued partial records or lost cleanup: %q %v", got, err)
			}
			if c.observers.registrations != 0 || c.observers.optionsBytes != 0 || c.observers.pendingRecords != 0 || len(c.observers.transientTargets) != 0 {
				t.Fatal("failed transient admission retained state")
			}
		})
	}
}

func TestMutationObserverSameParentAndSelfReferenceProduceRealMoveRecords(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="parent"><i id="a"></i><b id="b"></b></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const parent = document.getElementById('parent'), a = document.getElementById('a'), b = document.getElementById('b');
		const observer = new MutationObserver(() => {}); observer.observe(parent, {childList:true});
		parent.insertBefore(a, a); const self = observer.takeRecords(); parent.appendChild(a); const moved = observer.takeRecords(); observer.disconnect();
		return String(self.length === 2 && self[0].removedNodes[0] === a && self[0].nextSibling === b && self[1].addedNodes[0] === a && self[1].nextSibling === b &&
			moved.length === 2 && moved[0].removedNodes[0] === a && moved[0].nextSibling === b && moved[1].addedNodes[0] === a && moved[1].previousSibling === b && moved[1].nextSibling === null && parent.firstChild === b && parent.lastChild === a);
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("same-parent/self-reference move fabricated positions or omitted real records: %q %v", got, err)
	}
}

func TestMutationObserverChildWritersRejectForgedNodeArguments(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="parent"><i id="child"></i></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const parent = document.getElementById('parent'), child = document.getElementById('child');
		const observer = new MutationObserver(() => {}); observer.observe(parent, {childList:true});
		const forgedChild = {__id:child.__id}, forgedParent = {__id:parent.__id};
		let rejected = 0;
		for (const operation of [() => parent.appendChild(forgedChild), () => parent.removeChild(forgedChild),
			() => parent.insertBefore(child, {__id:999999}), () => parent.insertBefore(child),
			() => parent.appendChild.call(forgedParent, child), () => parent.removeChild.call(forgedParent, child),
			() => parent.insertBefore.call(forgedParent, child, null)]) {
			try { operation(); } catch (error) { if (!(error instanceof TypeError)) throw error; rejected++; }
		}
		const records = observer.takeRecords(); observer.disconnect();
		return String(rejected === 7 && records.length === 0 && child.parentNode === parent && parent.firstChild === child);
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("forged child mutation reached native tree ownership: %q %v", got, err)
	}
}

func TestMutationObserverNativeChildWritersRejectInvalidIdentities(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="parent"><i id="child"></i></div>`, nil)
	got, err := c.EvalString(t.Context(), `(() => {
		const parent = document.getElementById('parent'), child = document.getElementById('child');
		const observer = new MutationObserver(() => {}); observer.observe(parent, {childList:true});
		let rejected = 0, coerced = 0;
		const forged = {valueOf() { coerced++; throw Error('native identity coerced'); }};
		for (const operation of [() => __node_call(forged, 'appendChild', child.__id),
			() => __node_call(parent.__id, 'appendChild', forged), () => __node_call(parent.__id, 'removeChild', child.__id + 0.5),
			() => __node_call(parent.__id, 'insertBefore', child.__id, 999999), () => __node_call(parent.__id, 'insertBefore', child.__id, false),
			() => __node_call(parent.__id, 'appendChild'), () => __node_call(parent.__id, 'removeChild'), () => __node_call(parent.__id, 'insertBefore', child.__id)]) {
			try { operation(); } catch (error) { rejected++; }
		}
		const records = observer.takeRecords(); observer.disconnect();
		return String(rejected === 8 && coerced === 0 && records.length === 0 && child.parentNode === parent && parent.firstChild === child);
	})()`)
	if err != nil || got != "true" {
		t.Fatalf("invalid native identities changed tree ownership or invoked coercion: %q %v", got, err)
	}
}

func TestMutationObserverFailedTreePublicationPreservesTransientOwnership(t *testing.T) {
	c := newCtxFromHTML(t, `<div id="root"><i id="branch"></i></div>`, nil)
	if err := c.EvalDiscard(t.Context(), `globalThis.treeDeliveries = 0; globalThis.treeObserver = new MutationObserver(() => treeDeliveries++);
		treeObserver.observe(document.getElementById('root'), {childList:true, attributes:true, subtree:true});`); err != nil {
		t.Fatal(err)
	}
	root, err := c.doc.QuerySelector("#root")
	if err != nil || root == nil || root.FirstChild() == nil {
		t.Fatalf("root/child: %v", err)
	}
	if err := c.removeObservedChild(root, root.FirstChild()); err != nil {
		t.Fatal(err)
	}
	var owner *observer
	for _, candidate := range c.observers.by {
		owner = candidate
	}
	if owner == nil || len(owner.pending) != 1 || len(owner.transients) != 1 {
		t.Fatal("real removal admission missing")
	}
	owner.pending[0].PreviousSiblingID = math.MaxUint32
	pageBefore, internalBefore := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount()
	if err := c.EvalDiscard(t.Context(), "undefined"); err == nil || !strings.Contains(err.Error(), "sibling exceeds native bound") {
		t.Fatalf("failed publication became success: %v", err)
	}
	if c.observers.pendingRecords != 1 || c.observers.registrations != 2 || len(owner.transients) != 1 {
		t.Fatal("failed publication retired pending/transient ownership")
	}
	if page, internal := c.v8ctx.RetainedValueCount(), c.rt.iso.RetainedValueCount(); page != pageBefore || internal != internalBefore {
		t.Fatalf("failed publication leaked handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
	owner.pending[0].PreviousSiblingID = 0
	if err := c.EvalDiscard(t.Context(), "treeObserver.takeRecords(); treeObserver.disconnect();"); err != nil {
		t.Fatal(err)
	}
	if c.observers.pendingRecords != 0 || c.observers.registrations != 0 || c.observers.optionsBytes != 0 || len(c.observers.transientTargets) != 0 {
		t.Fatal("successful recovery retained pending/transient state")
	}
	if got, err := c.EvalString(t.Context(), "String(treeDeliveries)"); err != nil || got != "0" {
		t.Fatalf("failed/drained record reached callback: %q %v", got, err)
	}
}
