package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryCommandHelpFlagHasPublicDocumentation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "documentation.md"))
	if err != nil {
		t.Fatal(err)
	}
	document := string(raw)
	for _, command := range []struct {
		name string
		run  func([]string) int
	}{
		{"fetch", cmdFetch}, {"download", cmdDownload}, {"sitemap", cmdSitemap}, {"run", cmdRun}, {"serve", cmdServe}, {"mcp", cmdMCP},
		{"control", cmdControl}, {"observe", cmdObserve}, {"query", cmdQuery}, {"act", cmdAct},
		{"session", cmdSession}, {"profile", cmdProfile}, {"doctor", cmdDoctor}, {"playwright", cmdPlaywright}, {"trace", cmdTrace},
		{"diagnostics", cmdDiagnostics}, {"benchmark", cmdBenchmark},
	} {
		t.Run(command.name, func(t *testing.T) {
			output, err := os.Create(filepath.Join(t.TempDir(), "help"))
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestResource(t, "help capture close", output.Close)
			func() {
				original := os.Stderr
				os.Stderr = output
				defer func() { os.Stderr = original }()
				code := command.run([]string{"--help"})
				wantCode := 2
				if command.name == "benchmark" {
					wantCode = 0
				}
				if code != wantCode {
					t.Fatalf("help exit code=%d, expected inherited contract %d", code, wantCode)
				}
			}()
			help, err := os.ReadFile(output.Name())
			if err != nil {
				t.Fatal(err)
			}
			var scope strings.Builder
			for _, paragraph := range strings.Split(document, "\n\n") {
				if strings.Contains(paragraph, "`"+command.name+"`") || strings.Contains(paragraph, "`artemis "+command.name+" ") {
					scope.WriteString(paragraph)
				}
			}
			flags := 0
			for _, line := range strings.Split(string(help), "\n") {
				if !strings.HasPrefix(line, "  -") {
					continue
				}
				fields := strings.Fields(line)
				if len(fields) == 0 {
					t.Fatal("empty flag help entry")
				}
				flag := "-" + fields[0]
				if !strings.Contains(scope.String(), flag) {
					t.Errorf("public %s documentation omits actual flag %s", command.name, flag)
				}
				flags++
			}
			if flags == 0 {
				t.Fatal("command help exposed no flag contract")
			}
		})
	}
}
