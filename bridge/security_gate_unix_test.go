//go:build darwin || linux

package bridge

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestSecurityGatePolicyProxyHasNoGoroutineOrFileDescriptorLeak(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, "ok")
	}))
	defer backend.Close()
	baselineFDs, err := securityGateOpenFileDescriptors()
	if err != nil {
		t.Fatal(err)
	}
	baselineGoroutines := runtime.NumGoroutine()
	for iteration := 0; iteration < 12; iteration++ {
		policy := newProxyTestPolicy(t, backend.URL, true)
		proxy, err := newChromiumPolicyProxy(context.Background(), policy)
		if err != nil {
			t.Fatal(err)
		}
		proxyURL, err := url.Parse(proxy.URL())
		if err != nil {
			t.Fatal(err)
		}
		transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
		client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, backend.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, copyErr := io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		transport.CloseIdleConnections()
		proxyErr := proxy.Close()
		if copyErr != nil || closeErr != nil || proxyErr != nil {
			t.Fatalf("iteration %d cleanup: copy=%v body=%v proxy=%v", iteration, copyErr, closeErr, proxyErr)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		runtime.GC()
		currentFDs, err := securityGateOpenFileDescriptors()
		if err != nil {
			t.Fatal(err)
		}
		currentGoroutines := runtime.NumGoroutine()
		if currentFDs <= baselineFDs+2 && currentGoroutines <= baselineGoroutines+2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("proxy lifecycle leaked resources: fds=%d baseline=%d goroutines=%d baseline=%d", currentFDs, baselineFDs, currentGoroutines, baselineGoroutines)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func securityGateOpenFileDescriptors() (int, error) {
	var limit unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &limit); err != nil {
		return 0, fmt.Errorf("read file descriptor limit: %w", err)
	}
	const scanLimit = uint64(4096)
	if limit.Cur > scanLimit {
		limit.Cur = scanLimit
	}
	open := 0
	for descriptor := uint64(0); descriptor < limit.Cur; descriptor++ {
		if _, err := unix.FcntlInt(uintptr(descriptor), unix.F_GETFD, 0); err == nil {
			open++
		}
	}
	return open, nil
}
