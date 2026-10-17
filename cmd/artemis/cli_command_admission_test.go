package main

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
)

func TestInformationalCommandsRejectExtraArguments(t *testing.T) {
	if command := os.Getenv("ARTEMIS_TEST_INFORMATIONAL_COMMAND"); command != "" {
		os.Args = []string{"artemis", command, "ignored"}
		main()
		return
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"version", "-v", "--version", "help", "-h", "--help", "capabilities"} {
		child := exec.CommandContext(t.Context(), executable, "-test.run=^TestInformationalCommandsRejectExtraArguments$")
		child.Env = append(os.Environ(), "ARTEMIS_TEST_INFORMATIONAL_COMMAND="+command)
		var output, diagnostics bytes.Buffer
		child.Stdout, child.Stderr = &output, &diagnostics
		var exitErr *exec.ExitError
		if err := child.Run(); !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 || output.Len() != 0 || !strings.Contains(diagnostics.String(), "accepts no arguments") {
			t.Fatalf("%s did not reject extra arguments before output: err=%v out=%q stderr=%q", command, err, output.String(), diagnostics.String())
		}
	}
}

func TestControlRejectsUnknownCommandBeforeConnection(t *testing.T) {
	var hits atomic.Int64
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer peer.Close()
	endpoint := "ws" + strings.TrimPrefix(peer.URL, "http")
	for _, command := range []string{"", "not-real", "session.new ", "SESSION.NEW"} {
		if code := cmdControl([]string{"--server", endpoint, "--token", "test-token", "--cmd", command}); code != 2 || hits.Load() != 0 {
			t.Fatalf("unknown command reached control peer: code=%d hits=%d", code, hits.Load())
		}
	}
	for _, flag := range []string{"--session=", "--page=", "--snapshot-epoch=0"} {
		if code := cmdControl([]string{"--server", endpoint, "--token", "test-token", flag}); code != 2 || hits.Load() != 0 {
			t.Fatalf("explicit ignored flag reached control peer: %s code=%d hits=%d", flag, code, hits.Load())
		}
	}
}
