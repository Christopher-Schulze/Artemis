package actions

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Artemis/bridge"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	artemisdownload "github.com/Christopher-Schulze/Artemis/download"
	"github.com/Christopher-Schulze/Artemis/network"
	browserprocess "github.com/Christopher-Schulze/Artemis/process"
)

type downloadPage struct {
	runtime   *Runtime
	page      *bridge.Page
	downloads *artemisdownload.DownloadManager
	ref       string
}

// downloadPages opens one page per path in a single browser context, each
// with its own runtime and download session, and a "Get" link to /file/<id>.
func downloadPages(t *testing.T, ids ...string) (map[string]*downloadPage, *bridge.BrowserContext) {
	t.Helper()
	binary, err := browserprocess.DiscoverBinary("")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	mux.HandleFunc("/page/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/page/"):]
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><title>%s</title><a aria-label="Get" download href="/file/%s">Get</a>`, id, id)
	})
	mux.HandleFunc("/file/", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Path[len("/file/"):]
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.bin"`, id))
		if id == "empty" {
			return
		}
		time.Sleep(300 * time.Millisecond)
		if _, err := w.Write([]byte("payload-" + id)); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	browser, err := bridge.LaunchChromium(ctx, browserprocess.LaunchConfig{BinaryPath: binary.Path, Headless: true, AllowPrivateNetworks: true, AllowedPorts: []int{actionTestURLPort(t, server.URL)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := browser.Close(); err != nil {
			t.Error(err)
		}
	})
	owner, err := browser.NewContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := network.NewPolicy(network.DefaultPolicyConfig(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pages := map[string]*downloadPage{}
	for _, id := range ids {
		page, err := owner.NewPage(ctx, "about:blank")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err = page.Navigate(ctx, server.URL+"/page/"+id); err != nil {
			t.Fatal(err)
		}
		waitRuntimeReady(t, ctx, page)
		observer, err := bridgeobserve.NewCollector(page, bridgeobserve.DefaultConfig())
		if err != nil {
			t.Fatal(err)
		}
		downloads, err := artemisdownload.NewDownloadManager(artemisdownload.DownloadConfig{RootDir: t.TempDir(), SessionID: "dl-" + id, Policy: policy})
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := NewRuntimeWithConfig(page, observer, nil, RuntimeConfig{Downloads: downloads, DefaultTimeout: 30 * time.Second, UserID: "download-owner"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := runtime.Close(); err != nil {
				t.Error(err)
			}
		})
		snapshot, err := observer.Capture(ctx, bridgeobserve.ModeFull, "")
		if err != nil {
			t.Fatal(err)
		}
		entry := &downloadPage{runtime: runtime, page: page, downloads: downloads}
		for _, node := range snapshot.Nodes {
			if node.Name == "Get" && node.Role == "link" {
				entry.ref = node.Ref
			}
		}
		if entry.ref == "" {
			t.Fatalf("page %s has no download link", id)
		}
		pages[id] = entry
	}
	return pages, owner
}

func TestConcurrentPagesInOneContextKeepTheirOwnDownloads(t *testing.T) {
	pages, _ := downloadPages(t, "alpha", "beta")
	var group sync.WaitGroup
	outcomes := map[string]Outcome{}
	var mu sync.Mutex
	for id, entry := range pages {
		group.Add(1)
		go func() {
			defer group.Done()
			out := entry.runtime.Execute(context.Background(), Request{Kind: KindDownload, Ref: entry.ref, Timeout: 20 * time.Second})
			mu.Lock()
			outcomes[id] = out
			mu.Unlock()
		}()
	}
	group.Wait()
	for id, out := range outcomes {
		if !out.Success || out.Download == nil {
			t.Fatalf("%s download failed: %+v", id, out)
		}
		raw, err := os.ReadFile(out.Download.Path)
		if err != nil || string(raw) != "payload-"+id || out.Download.Filename != id+".bin" {
			t.Fatalf("%s received a foreign artifact: %q %s %v", id, raw, out.Download.Filename, err)
		}
	}
}

func TestEmptyDownloadFailsWithoutArtifactAndForeignFramesAreNotOwned(t *testing.T) {
	pages, _ := downloadPages(t, "empty")
	entry := pages["empty"]
	out := entry.runtime.Execute(context.Background(), Request{Kind: KindDownload, Ref: entry.ref, Timeout: 20 * time.Second})
	if out.Success || out.Failure != FailurePostcondition || out.Download != nil || out.Evidence.Postcondition.Passed {
		t.Fatalf("empty download reported success or kept an artifact: %+v", out)
	}
	files, err := os.ReadDir(entry.downloads.Directory())
	if err != nil || len(files) != 0 {
		t.Fatalf("empty download left committed files: %v %v", files, err)
	}
	ctx := context.Background()
	if owned, err := entry.page.OwnsFrame(ctx, entry.page.TargetID()); err != nil || !owned {
		t.Fatalf("main frame not owned: %v %v", owned, err)
	}
	if owned, err := entry.page.OwnsFrame(ctx, "FOREIGN-FRAME"); err != nil || owned {
		t.Fatalf("foreign frame owned: %v %v", owned, err)
	}
}
