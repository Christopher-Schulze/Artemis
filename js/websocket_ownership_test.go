package js

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/coder/websocket"
)

func TestWebSocketListenerFailureUsesTheExistingEventErrorOwner(t *testing.T) {
	target, stop := startEchoWS(t)
	defer stop()
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: localWebSocketPolicy(t, target, nil)})
	err := c.EvalDiscard(t.Context(), `
		globalThis.observedSocket = new WebSocket(`+jsStringLit(target)+`);
		observedSocket.onopen = () => { throw Error('actual-websocket-listener-failure'); };
		observedSocket.addEventListener('open', () => { document.title = 'subsequent-listener-ran'; }, {once:true});
	`)
	deadline := time.Now().Add(time.Second)
	for err == nil && time.Now().Before(deadline) {
		err = c.WaitIdle(t.Context())
		if err == nil {
			time.Sleep(time.Millisecond)
		}
	}
	var listenerError *EventListenerError
	if !errors.As(err, &listenerError) || !strings.Contains(err.Error(), "actual-websocket-listener-failure") {
		t.Fatalf("real WebSocket listener failure became success: %v", err)
	}
	if c.doc.Title() != "subsequent-listener-ran" {
		t.Fatal("failed WebSocket property handler prevented its subsequent listener")
	}
	assertExecutionRecovery(t, c)
}

func TestWebSocketEventTargetsKeepListenerIdentityAndOptions(t *testing.T) {
	target, stop := startEchoWS(t)
	defer stop()
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: localWebSocketPolicy(t, target, nil)})
	got, err := c.EvalString(t.Context(), `
		const first = new WebSocket(`+jsStringLit(target)+`), second = new WebSocket(`+jsStringLit(target)+`);
		const trace = [], removed = () => trace.push('removed');
		for (const [socket, label] of [[first, 'a'], [second, 'b']]) {
			socket.addEventListener('probe', () => trace.push(label + '-once'), {once:true});
			socket.addEventListener('probe', {handleEvent(event) {
				if (event.target !== socket || event.currentTarget !== socket) throw Error('wrong target');
				trace.push(label + '-object');
			}});
			socket.addEventListener('probe', removed, true);
			socket.removeEventListener('probe', removed, true);
		}
		first.dispatchEvent(new Event('probe')); second.dispatchEvent(new Event('probe'));
		first.dispatchEvent(new Event('probe')); trace.join('|');
	`)
	if err != nil || got != "a-once|a-object|b-once|b-object|a-object" {
		t.Fatalf("WebSocket targets shared listeners or lost event options: %q %v", got, err)
	}
}

func TestWebSocketCloseJoinsActualOpeningWorker(t *testing.T) {
	target, stop := startEchoWS(t)
	defer stop()
	entered, allowReturn := make(chan struct{}), make(chan struct{})
	var enteredOnce, allow sync.Once
	defer allow.Do(func() { close(allowReturn) })
	policy := localWebSocketPolicy(t, target, func(decision network.Decision) error {
		enteredOnce.Do(func() { close(entered) })
		<-allowReturn
		return nil
	})
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: policy})
	if _, err := c.EvalString(t.Context(), `globalThis.ownedSocket = new WebSocket(`+jsStringLit(target)+`); 'opening'`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("actual opening worker never reached policy admission")
	}
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	<-c.async.ctx.Done()
	select {
	case <-closed:
		t.Error("Close acknowledged while its opening worker was still active")
	case <-time.After(20 * time.Millisecond):
	}
	allow.Do(func() { close(allowReturn) })
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close did not join actual opening-worker completion")
	}
}

func TestWebSocketBinaryEchoUsesActualBufferRegion(t *testing.T) {
	target, stop := startEchoWS(t)
	defer stop()
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: localWebSocketPolicy(t, target, nil)})
	if err := c.EvalDiscard(t.Context(), `
		globalThis.binaryTrace='';
		const socket=new WebSocket(`+jsStringLit(target)+`);
		socket.binaryType='arraybuffer';
		socket.onopen=()=>socket.send(new Uint8Array([99,0,255,128,0,77]).subarray(1,5));
		socket.onmessage=event=>{
			binaryTrace=(event.data instanceof ArrayBuffer)+':'+JSON.stringify(Array.from(new Uint8Array(event.data)));
			socket.close();
		};
		socket.onclose=()=>{binaryTrace+=':closed'};
	`); err != nil {
		t.Fatal(err)
	}
	if got := waitWebSocketTrace(t, c, "binaryTrace", ":closed"); got != "true:[0,255,128,0]:closed" {
		t.Fatalf("actual WebSocket echo lost binary frame type, bytes or view offsets: %q", got)
	}
}

func TestWebSocketPayloadBindingFailureCannotBecomeSuccess(t *testing.T) {
	target, stop := startEchoWS(t)
	defer stop()
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: localWebSocketPolicy(t, target, nil)})
	if err := c.EvalDiscard(t.Context(), `
		Object.defineProperty(globalThis,'__wsLastPayload',{set(){throw Error('actual-payload-binding-failure')}});
		const socket=new WebSocket(`+jsStringLit(target)+`);
		socket.onopen=()=>socket.send('actual payload');
	`); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	var failure error
	for failure == nil && time.Now().Before(deadline) {
		failure = c.WaitIdle(t.Context())
		if failure == nil {
			time.Sleep(time.Millisecond)
		}
	}
	if failure == nil || !strings.Contains(failure.Error(), "actual-payload-binding-failure") {
		t.Fatalf("actual payload binding failure was lost: %v", failure)
	}
}

func TestWebSocketTerminalEventSurvivesSaturatedCancelledTransport(t *testing.T) {
	registry := newWSRegistry()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	conn := &wsConn{ctx: ctx, cancel: cancel}
	id := registry.put(conn)
	for index := 0; index < cap(registry.events); index++ {
		if !registry.tryEvent(ctx, wsEvent{connID: id, kind: wsMessage, data: []byte{byte(index)}}) {
			t.Fatal("actual event queue did not accept its declared capacity")
		}
	}
	cancel()
	if !registry.tryTerminalEvent(wsEvent{connID: id, kind: wsClose, code: 4001, reason: "actual close"}) {
		t.Fatal("terminal event was lost under queue saturation after transport cancellation")
	}
	for range cap(registry.events) {
		<-registry.events
	}
	terminal, exists := registry.nextTerminalEvent()
	if !exists || terminal.kind != wsClose || terminal.code != 4001 || terminal.reason != "actual close" {
		t.Fatalf("terminal owner lost identity, content or once-only delivery: %+v", terminal)
	}
	if _, exists := registry.nextTerminalEvent(); exists {
		t.Fatal("terminal event was delivered more than once")
	}
}

func TestWebSocketRemoteClosePreservesActualCodeAndReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := websocket.Accept(writer, request, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Error(err)
			return
		}
		if err := conn.Close(4001, "actual remote reason"); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	target := strings.Replace(server.URL, "http://", "ws://", 1)
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: localWebSocketPolicy(t, target, nil)})
	if err := c.EvalDiscard(t.Context(), `globalThis.remoteClose='';const socket=new WebSocket(`+jsStringLit(target)+`);socket.onclose=event=>{remoteClose=event.code+':'+event.reason+':'+event.wasClean+':closed'}`); err != nil {
		t.Fatal(err)
	}
	if got := waitWebSocketTrace(t, c, "remoteClose", ":closed"); got != "4001:actual remote reason:true:closed" {
		t.Fatalf("actual remote close was replaced by synthetic success: %q", got)
	}
}

func TestWebSocketRegistryBoundsAdmissionAndJoinsWorkers(t *testing.T) {
	registry := newWSRegistry()
	for range maxWebSocketConnections {
		if id := registry.put(&wsConn{}); id == 0 {
			t.Fatal("connection denied before actual capacity")
		}
	}
	if registry.put(&wsConn{}) != 0 {
		t.Fatal("connection admission exceeded its declared bound")
	}
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	for range maxWebSocketWorkers {
		if !registry.start(func() { <-release }) {
			t.Fatal("worker denied before actual capacity")
		}
	}
	if registry.start(func() { t.Error("denied work actually executed") }) {
		t.Fatal("worker admission exceeded its declared bound")
	}
	once.Do(func() { close(release) })
	registry.closeAll()
	if registry.activeWorkers != 0 || registry.start(func() { t.Error("closed work executed") }) || registry.put(&wsConn{}) != 0 {
		t.Fatal("closed registry acknowledged unjoined work or admitted new work")
	}
}

func TestWebSocketCloseDoesNotHoldTheIsolateForPeerHandshake(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := websocket.Accept(writer, request, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Error(err)
			return
		}
		<-release
		if err := conn.CloseNow(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Error(err)
		}
	}))
	defer server.Close()
	defer close(release)
	target := strings.Replace(server.URL, "http://", "ws://", 1)
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: localWebSocketPolicy(t, target, nil)})
	if err := c.EvalDiscard(t.Context(), `globalThis.closeTrace='';globalThis.slowPeerSocket=new WebSocket(`+jsStringLit(target)+`);slowPeerSocket.onopen=()=>{closeTrace='open'}`); err != nil {
		t.Fatal(err)
	}
	waitWebSocketTrace(t, c, "closeTrace", "open")
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	if got, err := c.EvalString(ctx, `slowPeerSocket.close(); 'isolate-released'`); err != nil || got != "isolate-released" {
		t.Fatalf("peer close handshake held the actual isolate: %q %v", got, err)
	}
	c.Close()
	if c.ws.activeWorkers != 0 {
		t.Fatal("Context close acknowledged before handshake worker retirement")
	}
}

func TestWebSocketSendsPreserveCallOrder(t *testing.T) {
	target, stop := startEchoWS(t)
	defer stop()
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: localWebSocketPolicy(t, target, nil)})
	if err := c.EvalDiscard(t.Context(), `
		globalThis.orderedMessages=[]; globalThis.orderedTrace='';
		const socket=new WebSocket(`+jsStringLit(target)+`);
		socket.onopen=()=>{for(let index=0;index<50;index++)socket.send(String(index))};
		socket.onmessage=event=>{orderedMessages.push(event.data);if(orderedMessages.length===50){orderedTrace=orderedMessages.every((value,index)=>value===String(index))?'ordered':'reordered';socket.close()}};
		socket.onclose=()=>{orderedTrace+=':closed'};
	`); err != nil {
		t.Fatal(err)
	}
	if got := waitWebSocketTrace(t, c, "orderedTrace", ":closed"); got != "ordered:closed" {
		t.Fatalf("actual wire messages violated JavaScript call order: %q", got)
	}
}

func TestWebSocketNegotiatesActualSubprotocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := websocket.Accept(writer, request, &websocket.AcceptOptions{InsecureSkipVerify: true, Subprotocols: []string{"actual.v1"}})
		if err != nil {
			t.Error(err)
			return
		}
		defer func() {
			if err := conn.CloseNow(); err != nil && !errors.Is(err, net.ErrClosed) {
				t.Error(err)
			}
		}()
		if err := conn.Write(request.Context(), websocket.MessageText, []byte(conn.Subprotocol())); err != nil {
			t.Error(err)
		}
		for {
			if _, _, err := conn.Read(request.Context()); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	target := strings.Replace(server.URL, "http://", "ws://", 1)
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: localWebSocketPolicy(t, target, nil)})
	if err := c.EvalDiscard(t.Context(), `globalThis.negotiated=''; const socket=new WebSocket(`+jsStringLit(target)+`,new Set(['actual.v1']));socket.onmessage=event=>{negotiated=socket.protocol+':'+event.data;socket.close()};socket.onclose=()=>{negotiated+=':closed'}`); err != nil {
		t.Fatal(err)
	}
	if got := waitWebSocketTrace(t, c, "negotiated", ":closed"); got != "actual.v1:actual.v1:closed" {
		t.Fatalf("requested subprotocol never reached the actual handshake: %q", got)
	}
}

func TestWebSocketRejectsInvalidProtocolsBeforeTransport(t *testing.T) {
	target, stop := startEchoWS(t)
	defer stop()
	var admissions atomic.Int32
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: localWebSocketPolicy(t, target, func(network.Decision) error { admissions.Add(1); return nil })})
	got, err := c.EvalString(t.Context(), `(() => {
		let rejected=0;
		for(const protocols of [['same','same'],[''],['bad token'],42,[Symbol('invalid')],Array.from({length:33},(_,i)=>'p'+i),['x'.repeat(257)]]) {
			try { new WebSocket(`+jsStringLit(target)+`,protocols) } catch(error) { rejected++ }
		}
		return String(rejected);
	})()`)
	if err != nil || got != "7" || admissions.Load() != 0 || len(c.ws.conns) != 0 {
		t.Fatalf("invalid protocol input admitted transport: rejected=%q err=%v admissions=%d connections=%d", got, err, admissions.Load(), len(c.ws.conns))
	}
}

func TestWebSocketRejectsMissingOrCaseChangedPeerProtocol(t *testing.T) {
	for _, selected := range []string{"", "ACTUAL.V1"} {
		t.Run(selected, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				options := &websocket.AcceptOptions{InsecureSkipVerify: true}
				if selected != "" {
					writer.Header().Set("Sec-WebSocket-Protocol", selected)
				}
				conn, err := websocket.Accept(writer, request, options)
				if err != nil {
					t.Error(err)
					return
				}
				defer func() {
					if err := conn.CloseNow(); err != nil && !errors.Is(err, net.ErrClosed) {
						t.Error(err)
					}
				}()
				for {
					if _, _, err := conn.Read(request.Context()); err != nil {
						return
					}
				}
			}))
			defer server.Close()
			target := strings.Replace(server.URL, "http://", "ws://", 1)
			c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: localWebSocketPolicy(t, target, nil)})
			if err := c.EvalDiscard(t.Context(), `globalThis.peerProtocolTrace='';const socket=new WebSocket(`+jsStringLit(target)+`,'actual.v1');socket.onopen=()=>{peerProtocolTrace+='invalid-open:'};socket.onerror=()=>{peerProtocolTrace+='error:'};socket.onclose=()=>{peerProtocolTrace+='closed'}`); err != nil {
				t.Fatal(err)
			}
			if got := waitWebSocketTrace(t, c, "peerProtocolTrace", "closed"); got != "error:closed" {
				t.Fatalf("peer protocol mismatch became an open connection: %q", got)
			}
		})
	}
}

func TestWebSocketCloseValidatesBeforeChangingState(t *testing.T) {
	target, stop := startEchoWS(t)
	defer stop()
	c := newCtxFromHTMLOpts(t, "<html></html>", ContextOpts{Policy: localWebSocketPolicy(t, target, nil)})
	if err := c.EvalDiscard(t.Context(), `
		globalThis.closeValidationTrace='';
		const socket=new WebSocket(`+jsStringLit(target)+`);
		let rejected=0;
		for(const [code,reason] of [[0,''],[-1,''],[1001,''],[5000,''],[NaN,''],[Infinity,''],[1000,'x'.repeat(124)],[1000,'ä'.repeat(62)]]){
			try {socket.close(code,reason)} catch(error){if(socket.readyState===0 && error instanceof DOMException)rejected++}
		}
		socket.onopen=()=>socket.close(2999.5,0);
		socket.onclose=event=>{closeValidationTrace=rejected+':'+event.code+':'+event.reason+':closed'};
	`); err != nil {
		t.Fatal(err)
	}
	if got := waitWebSocketTrace(t, c, "closeValidationTrace", ":closed"); got != "8:3000:0:closed" {
		t.Fatalf("close validation lost state, Web IDL clamp, UTF-8 bound or falsy reason: %q", got)
	}
}
