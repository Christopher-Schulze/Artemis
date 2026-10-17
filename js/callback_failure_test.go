package js

import (
	"errors"
	"strings"
	"testing"
)

func TestCompatibilityCallbacksCannotSilentlyAcknowledgeFailure(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"idle", `requestIdleCallback(()=>{throw Error('actual-callback-failure')})`},
		{"broadcast", `let a=new BroadcastChannel('actual'),b=new BroadcastChannel('actual');b.onmessage=()=>{throw Error('actual-callback-failure')};a.postMessage(1)`},
		{"message-property", `let c=new MessageChannel();c.port2.onmessage=()=>{throw Error('actual-callback-failure')};c.port1.postMessage(1)`},
		{"message-listener", `let c=new MessageChannel();c.port2.addEventListener('message',()=>{throw Error('actual-callback-failure')});c.port1.postMessage(1)`},
		{"message-dispatch", `let c=new MessageChannel();c.port2.addEventListener('message',()=>{throw Error('actual-callback-failure')});try{c.port2.dispatchEvent(new Event('message'))}catch{}`},
		{"intersection", `new IntersectionObserver(()=>{throw Error('actual-callback-failure')}).observe(document.body)`},
		{"resize", `new ResizeObserver(()=>{throw Error('actual-callback-failure')}).observe(document.body)`},
		{"custom-element", `function Actual(){};Actual.prototype.connectedCallback=()=>{throw Error('actual-callback-failure')};try{customElements.define('actual-element',Actual)}catch{}`},
		{"file-text", `let r=new FileReader();r.onload=()=>{throw Error('actual-callback-failure')};r.readAsText(new Blob(['actual']))`},
		{"file-binary", `let r=new FileReader();r.onload=()=>{throw Error('actual-callback-failure')};r.readAsArrayBuffer(new Blob(['actual']))`},
		{"canvas", `try{document.createElement('canvas').toBlob(()=>{throw Error('actual-callback-failure')})}catch{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := newCtxFromHTML(t, `<body><actual-element></actual-element></body>`, nil)
			err := c.EvalDiscard(t.Context(), test.source)
			if err == nil || !strings.Contains(err.Error(), "actual-callback-failure") {
				t.Fatalf("actual callback failure acknowledged success: %v", err)
			}
			assertExecutionRecovery(t, c)
		})
	}
}

func TestCompatibilityCallbackFailuresPreserveLaterDelivery(t *testing.T) {
	c := newCtxFromHTML(t, `<body><actual-element></actual-element><actual-element></actual-element></body>`, nil)
	err := c.EvalDiscard(t.Context(), `
globalThis.actualDeliveries=0;
let channel=new MessageChannel();
channel.port2.onmessage=()=>{actualDeliveries++;throw Error('actual-property-failure')};
channel.port2.addEventListener('message',()=>{actualDeliveries++;throw Error('actual-listener-failure')});
channel.port2.addEventListener('message',()=>actualDeliveries++);
channel.port1.postMessage(1);
function Actual(){};
Actual.prototype.connectedCallback=function(){actualDeliveries++;throw Error('actual-element-failure')};
customElements.define('actual-element',Actual);
`)
	var listener *EventListenerError
	if !errors.As(err, &listener) {
		t.Fatalf("callback errors lost their existing event owner: %v", err)
	}
	for _, marker := range []string{"actual-property-failure", "actual-listener-failure", "actual-element-failure"} {
		if !strings.Contains(err.Error(), marker) {
			t.Fatalf("callback failure disappeared: %s: %v", marker, err)
		}
	}
	if got, err := c.EvalString(t.Context(), `String(actualDeliveries)`); err != nil || got != "5" {
		t.Fatalf("one callback failure prevented remaining delivery: %q %v", got, err)
	}
}

func TestEmptyCallbackFailuresRemainObservable(t *testing.T) {
	for _, source := range []string{
		`addEventListener('actual',()=>{throw ''});dispatchEvent(new Event('actual'))`,
		`new ResizeObserver(()=>{throw ''}).observe(document.body)`,
		`try{document.createElement('canvas').toBlob(()=>{throw ''})}catch{}`,
	} {
		c := newCtxFromHTML(t, `<body></body>`, nil)
		var listener *EventListenerError
		if err := c.EvalDiscard(t.Context(), source); !errors.As(err, &listener) {
			t.Fatalf("empty callback exception disappeared: %v", err)
		}
		assertExecutionRecovery(t, c)
	}
}

func TestEventCallbackFailureOverflowRemainsObservable(t *testing.T) {
	c := newCtxFromHTML(t, `<body></body>`, nil)
	err := c.EvalDiscard(t.Context(), `
for (let index = 0; index < 20; index++) {
  const marker = 'overflow-callback-' + index;
  addEventListener('overflow', () => { throw Error(marker); });
}
dispatchEvent(new Event('overflow'));
`)
	var listener *EventListenerError
	if !errors.As(err, &listener) {
		t.Fatalf("callback overflow silently acknowledged success: %v", err)
	}
	if !strings.Contains(err.Error(), "overflow-callback-0") || !strings.Contains(err.Error(), "4 additional callback failures omitted") {
		t.Fatalf("bounded callback error summary lost detail or overflow count: %v", err)
	}
	if strings.Contains(err.Error(), "overflow-callback-16") {
		t.Fatalf("overflow marker included an error beyond the bounded detail limit: %v", err)
	}
}
