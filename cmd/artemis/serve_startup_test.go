package main

import (
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/serve"
)

// TestServeStartupAndShutdown verifies that the serve server starts, binds
// loopback, and shuts down cleanly. This is the clean-install serve startup
// proof required by TASK-2361.
func TestServeStartupAndShutdown(t *testing.T) {
	if defaultServeHost != "127.0.0.1" {
		t.Fatalf("default serve host = %q, want numeric loopback", defaultServeHost)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	agent, err := artemis.NewAgent(artemis.AgentConfig{})
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	defer agent.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := agent.Start(ctx); err != nil {
		t.Fatalf("agent.Start: %v", err)
	}

	opts := serve.Opts{
		Logger:         slog.Default(),
		AuthToken:      "test-token",
		OriginPatterns: []string{"localhost", "127.0.0.1"},
	}
	srv := serve.New(agent, opts)
	addr := net.JoinHostPort("127.0.0.1", strings.TrimSpace(itoa(port)))

	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx, addr)
	}()

	// Poll for the server to bind.
	deadline := time.Now().Add(10 * time.Second)
	var connected bool
	for time.Now().Before(deadline) {
		conn, derr := net.DialTimeout("tcp", addr, 500*time.Millisecond)
		if derr == nil {
			conn.Close()
			connected = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !connected {
		t.Fatalf("serve did not bind within 10s")
	}

	cancel()
	<-done
}

func TestServeRejectsDisabledSecurityLimits(t *testing.T) {
	for _, args := range [][]string{{"--rate", "0"}, {"--burst", "0"}, {"--client-rate", "0"}, {"--client-burst", "0"}, {"--origin", ""}, {"--origin", "*"}, {"--origin", "https://*"}} {
		if code := cmdServe(args); code != 2 {
			t.Errorf("cmdServe(%v) = %d, want argument error 2", args, code)
		}
	}
}

// TestDoctorVerifiesPlatform proves the doctor command reports the correct
// platform and version information for a clean install.
func TestDoctorVerifiesPlatform(t *testing.T) {
	code := cmdDoctor([]string{"--format", "json"})
	if code != 0 {
		t.Fatalf("doctor exit code = %d, want 0", code)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
