package js

import "testing"

func TestDocumentWindowAndElementHaveDistinctEventTargets(t *testing.T) {
	c := newCtxFromHTML(t, "<html><body><button id='b'>go</button></body></html>", nil)
	value, err := c.Eval(t.Context(), `
		(() => {
			const trace = [], button = document.getElementById('b');
			for (const [name, target] of [['window', window], ['document', document], ['html', document.documentElement], ['button', button]]) {
				target.addEventListener('probe', function(e) {
					trace.push(name + ':' + (e.target === button) + ':' + (e.currentTarget === this) + ':' + e.eventPhase);
				}, true);
				target.addEventListener('probe', function(e) { trace.push(name + '-bubble:' + e.eventPhase); });
			}
			const event = new Event('probe', {bubbles: true});
			button.dispatchEvent(event);
			if (event.currentTarget !== null || event.eventPhase !== 0) throw Error('dispatch state leaked');
			return trace.join('|');
		})()
	`)
	want := "window:true:true:1|document:true:true:1|html:true:true:1|button:true:true:2|button-bubble:2|html-bubble:3|document-bubble:3|window-bubble:3"
	if err != nil || value.String() != want {
		t.Fatalf("event target/propagation mismatch: got=%q want=%q err=%v", value.String(), want, err)
	}
}

func TestDocumentEventListenerMutationAndImmediateStop(t *testing.T) {
	c := newCtxFromHTML(t, "<html><body></body></html>", nil)
	value, err := c.Eval(t.Context(), `
		(() => {
			const trace = [], removed = () => trace.push('removed');
			document.addEventListener('probe', function(e) {
				trace.push('once');
				document.removeEventListener('probe', removed);
				document.addEventListener('probe', () => trace.push('added'));
			}, {once: true});
			document.addEventListener('probe', removed);
			document.dispatchEvent(new Event('probe'));
			document.dispatchEvent(new Event('probe'));
			document.addEventListener('stop', e => { trace.push('stop'); e.stopImmediatePropagation(); });
			document.addEventListener('stop', () => trace.push('unreachable'));
			document.dispatchEvent(new Event('stop'));
			return trace.join('|');
		})()
	`)
	if err != nil || value.String() != "once|added|stop" {
		t.Fatalf("listener mutation mismatch: got=%q err=%v", value.String(), err)
	}
}
