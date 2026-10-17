package js

import "testing"

func TestStorageCallbacksRetireNativeHandles(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	const exercise = `(() => {
		for (const storage of [localStorage, sessionStorage]) {
			for (let index=0; index<50; index++) {
				storage.setItem('actual', 'owned');
				if(storage.getItem('actual')!=='owned' || storage.key(0)!=='actual' || storage.lengthOf()!==1) throw Error('storage state lost');
				storage.removeItem('actual');
				storage.clear();
			}
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
		t.Fatalf("storage callbacks retained native handles: page=%d baseline=%d internal=%d baseline=%d", page, pageBefore, internal, internalBefore)
	}
}

func TestStorageCallbacksRejectForeignReceiverWithoutNativeAccess(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	got, err := c.EvalString(t.Context(), `(() => {
		localStorage.setItem('kept', 'actual');
		for(const method of ['getItem','setItem','removeItem','clear','key','lengthOf']) {
			localStorage[method].call({}, 'kept', 'forged');
		}
		return localStorage.getItem('kept')+':'+localStorage.lengthOf();
	})()`)
	if err != nil || got != "actual:1" {
		t.Fatalf("foreign storage receiver corrupted state or reached invalid native fields: %q %v", got, err)
	}
}
