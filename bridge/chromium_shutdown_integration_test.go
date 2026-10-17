package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
	"github.com/coder/websocket"
)

func TestOwnedChromiumGracefulTimeoutConfirmsRealProcessAndProfileCleanup(t *testing.T) {
	binary := requireChromium(t)
	process, err := browserprocess.Launch(context.Background(), browserprocess.LaunchConfig{BinaryPath: binary.Path, Headless: true, StartupTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "native process", process.Close)
	blocked := make(chan struct{})
	relayDone := make(chan struct{})
	var once sync.Once
	endpoint := cdpTestServer(t, func(parent context.Context, client *websocket.Conn) error {
		ctx, cancel := context.WithCancel(parent)
		defer cancel()
		defer close(relayDone)
		native, _, err := websocket.Dial(ctx, process.Endpoint(), nil)
		if err != nil {
			return err
		}
		defer closeBridgeTestResource(t, "native relay connection", native.CloseNow)
		client.SetReadLimit(defaultCDPMaxMessage)
		native.SetReadLimit(defaultCDPMaxMessage)
		forward := func(source, destination *websocket.Conn, withholdClose bool) error {
			for {
				kind, payload, err := source.Read(ctx)
				if err != nil {
					return err
				}
				if withholdClose {
					var message struct {
						Method string `json:"method"`
					}
					if err := json.Unmarshal(payload, &message); err != nil {
						return err
					}
					if message.Method == "Browser.close" {
						once.Do(func() { close(blocked) })
						continue
					}
				}
				if err := destination.Write(ctx, kind, payload); err != nil {
					return err
				}
			}
		}
		upstreamDone := make(chan error, 1)
		go func() { upstreamDone <- forward(native, client, false) }()
		downstreamErr := forward(client, native, true)
		cancel()
		upstreamErr := <-upstreamDone
		for _, err := range []error{downstreamErr, upstreamErr} {
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) && !errors.Is(err, io.EOF) && websocket.CloseStatus(err) < 0 {
				return err
			}
		}
		return nil
	})
	policy, err := network.NewPolicy(network.PolicyConfig{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	browser, err := connectChromium(t.Context(), endpoint, process, policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "owned browser", browser.Close)
	if browser.Stopped() {
		t.Fatal("live native process reported stopped")
	}
	results := make(chan error, 2)
	go func() { results <- browser.Close() }()
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("real graceful-close command did not reach the fault relay")
	}
	if browser.Stopped() {
		t.Fatal("unacknowledged close reported physical completion")
	}
	go func() { results <- browser.Close() }()
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("verified owned fallback returned a false shutdown failure: %v", err)
			}
		case <-time.After(25 * time.Second):
			t.Fatal("owned shutdown exceeded its actual fallback bounds")
		}
	}
	for name, done := range map[string]<-chan struct{}{"native process": process.Exited(), "native profile cleanup": process.Done(), "transport reader": browser.transport.readDone, "browser monitor": browser.monitorDone, "process monitor": browser.processDone, "relay": relayDone} {
		select {
		case <-done:
		default:
			t.Fatalf("shutdown returned before %s completed", name)
		}
	}
	if !browser.Stopped() || browser.Healthy() || !slices.Contains(browser.ProcessWarnings(), "chromium_graceful_close_timed_out") {
		t.Fatalf("wrong physical/warning state: stopped=%t healthy=%t warnings=%v", browser.Stopped(), browser.Healthy(), browser.ProcessWarnings())
	}
	if _, err := os.Stat(process.ProfileDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned profile cleanup was not real: %v", err)
	}
}

func TestExternalDefaultContextShutdownClosesOnlyItsOwnedTargets(t *testing.T) {
	host := launchPolicyTestBrowser(t, browserprocess.LaunchConfig{})
	defaultOwner, err := host.DefaultContext()
	if err != nil {
		t.Fatal(err)
	}
	sentinel, err := defaultOwner.NewPage(t.Context(), "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	external, err := ConnectChromium(t.Context(), host.Endpoint())
	if err != nil {
		t.Fatal(err)
	}
	defer closeBridgeTestResource(t, "external connection", external.Close)
	owner, err := external.DefaultContext()
	if err != nil {
		t.Fatal(err)
	}
	owned, err := owner.NewPage(t.Context(), "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	if err := external.Close(); err != nil {
		t.Fatal(err)
	}
	if !external.Stopped() || external.Owned() || !host.Healthy() {
		t.Fatalf("external shutdown crossed process ownership: stopped=%t owned=%t host=%t", external.Stopped(), external.Owned(), host.Healthy())
	}
	if value, err := nativeFetchValue(t.Context(), sentinel, "location.href"); err != nil || value != "about:blank" {
		t.Fatalf("unrelated native target was damaged: url=%q error=%v", value, err)
	}
	var unexpected struct {
		SessionID string `json:"sessionId"`
	}
	err = host.Transport().Call(t.Context(), "Target.attachToTarget", attachTargetParams{TargetID: owned.TargetID(), Flatten: true}, &unexpected)
	if err == nil || !IsCDPError(err, CDPErrorProtocol) {
		t.Fatalf("external default-context target survived actual cleanup: session=%q error=%v", unexpected.SessionID, err)
	}
	select {
	case <-host.process.Exited():
		t.Fatal("external attachment terminated the unowned native process")
	default:
	}
}

func TestFailedOwnedCleanupRemainsObservableAndCannotReplaceOwnership(t *testing.T) {
	initializer := NewBridgeInitializer(testBridgeInitConfig())
	if err := initializer.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	session := initializer.Session()
	runtime := session.Runtime
	defer func() {
		if err := runtime.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close failed-cleanup fixture: %v", err)
		}
	}()
	provider, ok := initializer.provider.(*LocalChromeProvider)
	if !ok {
		t.Fatal("native initializer did not use its real local provider")
	}
	if err := runtime.policyProxy.listener.Close(); err != nil {
		t.Fatal(err)
	}
	<-runtime.policyProxy.done
	err := initializer.Stop()
	if err == nil || !errors.Is(err, net.ErrClosed) {
		t.Fatalf("actual proxy service failure disappeared: %v", err)
	}
	if initializer.IsStarted() || runtime.Stopped() || initializer.Session() != session || provider.session != session || initializer.State() != BridgeStateError {
		t.Fatalf("failed cleanup lost ownership/reported live state: started=%t stopped=%t state=%s", initializer.IsStarted(), runtime.Stopped(), initializer.State())
	}
	if _, err := provider.Launch(t.Context(), ProviderConfig{Headless: true}); err == nil {
		t.Fatal("unresolved cleanup owner was replaced")
	}
	if err := initializer.Stop(); err == nil {
		t.Fatal("repeated stop erased its real cleanup failure")
	}
	for name, done := range map[string]<-chan struct{}{"native process": runtime.process.Exited(), "native profile cleanup": runtime.process.Done(), "transport reader": runtime.transport.readDone} {
		select {
		case <-done:
		default:
			t.Fatalf("failed shutdown left %s running", name)
		}
	}
}
