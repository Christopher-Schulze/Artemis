package bridge

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

const policyProxyCloseTimeout = 5 * time.Second

type chromiumPolicyProxy struct {
	policy     *network.Policy
	listener   net.Listener
	server     *http.Server
	transport  *http.Transport
	reverse    *httputil.ReverseProxy
	done       chan struct{}
	cancel     context.CancelFunc
	mu         sync.Mutex
	closed     bool
	handlers   sync.WaitGroup
	conns      map[net.Conn]struct{}
	serveErr   error
	cleanupErr error
	closeOnce  sync.Once
	closeErr   error
}

func newChromiumPolicyProxy(ctx context.Context, policy *network.Policy) (*chromiumPolicyProxy, error) {
	if ctx == nil {
		return nil, fmt.Errorf("chromium policy proxy: context required")
	}
	if policy == nil {
		return nil, fmt.Errorf("chromium policy proxy: policy required")
	}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("chromium policy proxy: listen: %w", err)
	}
	proxy := &chromiumPolicyProxy{
		policy: policy, listener: listener, done: make(chan struct{}), conns: make(map[net.Conn]struct{}),
	}
	lifetime, cancel := context.WithCancel(context.Background())
	proxy.cancel = cancel
	proxy.transport = &http.Transport{
		Proxy: nil, DialContext: policy.DialContextFor("chromium-proxy"), ForceAttemptHTTP2: true,
		MaxIdleConns: 32, MaxIdleConnsPerHost: 8, IdleConnTimeout: 30 * time.Second,
	}
	proxy.server = &http.Server{
		Handler: proxy, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
		BaseContext: func(net.Listener) context.Context { return lifetime },
		ErrorLog:    log.New(io.Discard, "", 0),
	}
	proxy.reverse = proxy.newReverseProxy()
	go proxy.serve()
	return proxy, nil
}

func (p *chromiumPolicyProxy) URL() string {
	return (&url.URL{Scheme: "http", Host: p.listener.Addr().String()}).String()
}

func (p *chromiumPolicyProxy) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		http.Error(writer, "policy proxy closed", http.StatusServiceUnavailable)
		return
	}
	p.handlers.Add(1)
	p.mu.Unlock()
	defer p.handlers.Done()
	if request.Method == http.MethodConnect {
		p.serveConnect(writer, request)
		return
	}
	if request.URL == nil || !request.URL.IsAbs() {
		http.Error(writer, "absolute proxy URL required", http.StatusBadRequest)
		return
	}
	if err := p.policy.ValidateRequest(request.Context(), request.URL.String(), request.Method, request.Header.Get("Content-Type"), request.ContentLength, network.TargetProxy, "chromium-proxy"); err != nil {
		http.Error(writer, "request blocked by network policy", http.StatusForbidden)
		return
	}
	p.reverse.ServeHTTP(writer, request)
}

func (p *chromiumPolicyProxy) newReverseProxy() *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(proxyRequest *httputil.ProxyRequest) {
			request := proxyRequest.Out
			request.RequestURI = ""
			request.Host = request.URL.Host
			request.Header.Del("Proxy-Authorization")
			request.Header.Del("Proxy-Connection")
			request.Header["X-Forwarded-For"] = nil
		},
		Transport: p.transport,
		ErrorHandler: func(writer http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(writer, "request failed by policy proxy", http.StatusBadGateway)
		},
	}
}

func (p *chromiumPolicyProxy) serveConnect(writer http.ResponseWriter, request *http.Request) {
	targetURL := "https://" + request.Host
	if _, err := p.policy.ResolveURL(request.Context(), targetURL, network.TargetProxy, "chromium-proxy"); err != nil {
		http.Error(writer, "tunnel blocked by network policy", http.StatusForbidden)
		return
	}
	upstream, err := p.policy.DialContextFor("chromium-proxy")(request.Context(), "tcp", request.Host)
	if err != nil {
		http.Error(writer, "tunnel blocked by network policy", http.StatusForbidden)
		return
	}
	client, buffered, err := hijackProxyClient(writer)
	if err != nil {
		p.untrackAndClose(upstream)
		return
	}
	if !p.track(client, upstream) {
		p.untrackAndClose(client, upstream)
		return
	}
	defer p.untrackAndClose(client, upstream)
	if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	if err := buffered.Flush(); err != nil {
		return
	}
	copyTunnel(client, upstream)
}

func hijackProxyClient(writer http.ResponseWriter) (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("policy proxy: hijacking unavailable")
	}
	connection, buffered, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, fmt.Errorf("policy proxy: hijack: %w", err)
	}
	return connection, buffered, nil
}

func copyTunnel(client, upstream net.Conn) {
	done := make(chan struct{}, 1)
	go func() {
		_, _ = io.Copy(upstream, client)
		_ = upstream.SetDeadline(time.Now())
		done <- struct{}{}
	}()
	_, _ = io.Copy(client, upstream)
	_ = client.SetDeadline(time.Now())
	<-done
}

func (p *chromiumPolicyProxy) serve() {
	err := p.server.Serve(p.listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		p.serveErr = fmt.Errorf("chromium policy proxy: serve: %w", err)
	}
	close(p.done)
}

func (p *chromiumPolicyProxy) track(connections ...net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	for _, connection := range connections {
		p.conns[connection] = struct{}{}
	}
	return true
}

func (p *chromiumPolicyProxy) untrackAndClose(connections ...net.Conn) {
	for _, connection := range connections {
		err := connection.Close()
		p.mu.Lock()
		if err == nil || errors.Is(err, net.ErrClosed) {
			delete(p.conns, connection)
		} else {
			p.conns[connection] = struct{}{}
			p.cleanupErr = errors.Join(p.cleanupErr, fmt.Errorf("close policy proxy connection: %w", err))
		}
		p.mu.Unlock()
	}
}

func (p *chromiumPolicyProxy) Close() error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		connections := make([]net.Conn, 0, len(p.conns))
		for connection := range p.conns {
			connections = append(connections, connection)
		}
		p.mu.Unlock()
		p.cancel()
		p.untrackAndClose(connections...)
		ctx, cancel := context.WithTimeout(context.Background(), policyProxyCloseTimeout)
		defer cancel()
		if err := p.server.Shutdown(ctx); err != nil {
			p.closeErr = errors.Join(p.closeErr, err, p.server.Close())
		}
		p.transport.CloseIdleConnections()
		<-p.done
		p.handlers.Wait()
		p.mu.Lock()
		p.closeErr = errors.Join(p.closeErr, p.serveErr, p.cleanupErr)
		p.mu.Unlock()
	})
	return p.closeErr
}
