package network

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testRequestLifecycle struct {
	beginErr  error
	finishErr error
	started   int
	finished  int
	bytes     int64
}

func (l *testRequestLifecycle) BeginRequest(ctx context.Context) (context.Context, func(int64) error, error) {
	l.started++
	if l.beginErr != nil {
		return nil, nil, l.beginErr
	}
	return ctx, func(bytes int64) error {
		l.finished++
		l.bytes += bytes
		return l.finishErr
	}, nil
}

func TestRequestLifecycleOwnsAdmissionAndResponseAccounting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("payload")) }))
	defer srv.Close()
	lifecycle := &testRequestLifecycle{}
	client := newTestClient(t, HTTPClientConfig{RequestLifecycle: lifecycle})
	if _, err := client.Do(context.Background(), Request{URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	if lifecycle.started != 1 || lifecycle.finished != 1 || lifecycle.bytes != int64(len("payload")) {
		t.Fatalf("lifecycle=%+v", lifecycle)
	}
}

func TestRequestLifecycleFailsClosedBeforeAndAfterTransport(t *testing.T) {
	denied := errors.New("admission denied")
	lifecycle := &testRequestLifecycle{beginErr: denied}
	client := newTestClient(t, HTTPClientConfig{RequestLifecycle: lifecycle})
	if _, err := client.Do(context.Background(), Request{URL: "https://example.test/"}); !errors.Is(err, denied) {
		t.Fatalf("admission error=%v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	accounting := errors.New("accounting denied")
	lifecycle = &testRequestLifecycle{finishErr: accounting}
	client = newTestClient(t, HTTPClientConfig{RequestLifecycle: lifecycle})
	if response, err := client.Do(context.Background(), Request{URL: srv.URL}); !errors.Is(err, accounting) || response != nil {
		t.Fatalf("response=%v accounting error=%v", response, err)
	}
}
