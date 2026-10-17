package serve

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	artemis "github.com/Christopher-Schulze/Artemis"
)

func TestOwnedReadinessSeparatesFourRealServers(t *testing.T) {
	ready := make(chan Readiness, 4)
	for index := 0; index < cap(ready); index++ {
		agent, err := artemis.NewAgent(testAgentConfig(t))
		if err != nil {
			t.Fatal(err)
		}
		if err := agent.Start(t.Context()); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		server := New(agent, Opts{AuthToken: testAuthToken, Ready: func(info Readiness) error {
			ready <- info
			return nil
		}})
		done := make(chan error, 1)
		go func() { done <- server.ListenAndServe(ctx, "127.0.0.1:0") }()
		t.Cleanup(func() {
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("joined server shutdown: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("server owner was not joined")
			}
			if err := server.Close(); err != nil {
				t.Error(err)
			}
			stopTestAgent(t, agent)
		})
	}
	addresses, instances := make(map[string]bool), make(map[string]bool)
	for index := 0; index < cap(ready); index++ {
		var info Readiness
		select {
		case info = <-ready:
		case <-time.After(5 * time.Second):
			t.Fatal("real bound endpoint did not report readiness")
		}
		if err := info.Validate(); err != nil || info.ProcessID != os.Getpid() || addresses[info.Address] || instances[info.InstanceID] {
			t.Fatalf("invalid/shared endpoint ownership: %+v err=%v", info, err)
		}
		addresses[info.Address], instances[info.InstanceID] = true, true
		client, err := Dial(t.Context(), "ws://"+info.Address+"/", testAuthToken, "")
		if err != nil {
			t.Fatal(err)
		}
		if client.ServerInstanceID() != info.InstanceID || client.ServerProcessID() != info.ProcessID {
			t.Error("authenticated handshake differs from the actual listener owner")
		}
		if err := client.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadinessFailureClosesBoundListenerAndOccupiedPortNeverReportsReady(t *testing.T) {
	agent, err := artemis.NewAgent(testAgentConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer stopTestAgent(t, agent)
	reportErr := errors.New("real required output failure")
	var reported Readiness
	server := New(agent, Opts{AuthToken: testAuthToken, Ready: func(info Readiness) error {
		reported = info
		return reportErr
	}})
	if err := server.ListenAndServe(t.Context(), "127.0.0.1:0"); !errors.Is(err, reportErr) {
		t.Fatalf("readiness cause was lost: %v", err)
	}
	connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(t.Context(), "tcp", reported.Address)
	if err == nil {
		if err := connection.Close(); err != nil {
			t.Error(err)
		}
		t.Fatal("failed output left a bound listener")
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	occupied, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := occupied.Close(); err != nil {
			t.Error(err)
		}
	}()
	called := false
	server = New(agent, Opts{AuthToken: testAuthToken, Ready: func(Readiness) error { called = true; return nil }})
	if err := server.ListenAndServe(t.Context(), occupied.Addr().String()); err == nil || called {
		t.Fatalf("occupied foreign endpoint became readiness: called=%v err=%v", called, err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReadinessValidationRejectsIncompleteOrForeignIdentity(t *testing.T) {
	valid := Readiness{Address: "127.0.0.1:9333", ProcessID: 1, Protocol: ProtocolVersion, InstanceID: "instance"}
	for _, field := range []string{"address", "port", "pid", "protocol", "instance"} {
		value := valid
		switch field {
		case "address":
			value.Address = "192.0.2.1:9333"
		case "port":
			value.Address = "127.0.0.1:0"
		case "pid":
			value.ProcessID = 0
		case "protocol":
			value.Protocol = "unknown"
		case "instance":
			value.InstanceID = ""
		}
		if err := value.Validate(); err == nil {
			t.Fatalf("incomplete %s was accepted", field)
		}
	}
}
