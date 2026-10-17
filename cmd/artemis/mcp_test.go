package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPCommandServesStdioUntilThePeerCloses(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><title>CLI MCP</title><p>hello</p>`)
	}))
	defer page.Close()
	port := page.URL[strings.LastIndex(page.URL, ":")+1:]
	serverIn, clientOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	clientIn, serverOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalIn, originalOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = serverIn, serverOut
	defer func() { os.Stdin, os.Stdout = originalIn, originalOut }()
	t.Setenv("AGENT_BROWSER_ACTION_POLICY", writeCLIActionPolicy(t, `{"default":"allow"}`))
	exit := make(chan int, 1)
	go func() {
		exit <- cmdMCP([]string{"--chromium=false", "--block-private-ips=false", "--allowed-port", port})
	}()

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "cli-test", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.IOTransport{Reader: clientIn, Writer: clientOut}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if info := session.InitializeResult(); info == nil || info.ServerInfo.Name != "artemis" || info.ServerInfo.Version != Version {
		t.Fatalf("initialize = %+v", info)
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "artemis_open", Arguments: map[string]any{"url": page.URL}})
	if err != nil || result.IsError || !strings.Contains(result.Content[0].(*mcp.TextContent).Text, `"title":"CLI MCP"`) {
		t.Fatalf("open over stdio = %+v %v", result, err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-exit:
		if code != 0 {
			t.Fatalf("mcp exit code = %d", code)
		}
	case <-ctx.Done():
		t.Fatal("mcp did not exit after the peer closed stdio")
	}
}
