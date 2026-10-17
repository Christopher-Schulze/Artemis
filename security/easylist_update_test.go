package security

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDefaultEasyListUpdateConfig(t *testing.T) {
	cfg := DefaultEasyListUpdateConfig()
	if cfg.UpdateInterval != 24*time.Hour || cfg.MaxVersions != 3 || cfg.GradualRolloutPercent != 50 || cfg.GradualRolloutDelay != time.Hour || cfg.MaxBreakagesPerHour != 5 || cfg.MaxSizeMultiplier != 2.0 {
		t.Fatalf("spec defaults changed: %+v", cfg)
	}
	if len(cfg.Feeds) != 2 || len(cfg.Pins) == 0 {
		t.Fatalf("feeds or pins missing: %+v", cfg)
	}
	for _, feed := range cfg.Feeds {
		if !strings.HasPrefix(feed.Primary, "https://easylist.to/") || !strings.HasPrefix(feed.Mirror, "https://easylist-downloads.adblockplus.org/") {
			t.Fatalf("feed %s does not use the spec sources: %+v", feed.Name, feed)
		}
	}
}

type listServer struct {
	mu    sync.Mutex
	files map[string]string
	hits  map[string]int
}

func (s *listServer) set(path, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = body
}

func (s *listServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	body, ok := s.files[r.URL.Path]
	s.hits[r.URL.Path]++
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, err := w.Write([]byte(body)); err != nil {
		panic(err)
	}
}

func newListFixture(t *testing.T) (*listServer, EasyListUpdateConfig) {
	t.Helper()
	lists := &listServer{files: map[string]string{}, hits: map[string]int{}}
	server := httptest.NewTLSServer(lists)
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	sum := sha256.Sum256(server.Certificate().RawSubjectPublicKeyInfo)
	cfg := DefaultEasyListUpdateConfig()
	cfg.StorageDir = t.TempDir()
	cfg.RootCAs = roots
	cfg.Pins = []string{base64.StdEncoding.EncodeToString(sum[:])}
	cfg.Feeds = []EasyListFeed{{Name: "easylist", Category: AdBlockCategoryAd, Primary: server.URL + "/primary.txt", Mirror: server.URL + "/mirror.txt"}}
	return lists, cfg
}

func publishList(lists *listServer, rules string) {
	lists.set("/primary.txt", "[Adblock Plus 2.0]\n! Version: 1\n"+rules)
	lists.set("/mirror.txt", "[Adblock Plus 2.0]\n! Checksum: other-header\n! Version: 2\n"+rules)
}

func thirdPartyScript(url string) AdBlockRequest {
	host := strings.SplitN(strings.TrimPrefix(url, "https://"), "/", 2)[0]
	return AdBlockRequest{URL: url, Host: host, SiteHost: "news.example", Resource: ResourceScript, ThirdParty: true}
}

func TestEasyListUpdateVerifiesPinnedMirroredListAndRestoresFromCache(t *testing.T) {
	lists, cfg := newListFixture(t)
	publishList(lists, "||ads.fixture.test^\n@@||ads.fixture.test/allowed.js\n##.banner\n")
	blocker, err := NewAdBlockerWithConfig(nil, cfg.RolloutConfig())
	if err != nil {
		t.Fatal(err)
	}
	updater, err := NewEasyListUpdater(cfg, blocker)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := updater.Update(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	feed := manifest.Feeds[0]
	if manifest.Rules != 2 || feed.Stats.Cosmetic != 1 || feed.CompressedBytes <= 0 || feed.Bytes != len("||ads.fixture.test^\n@@||ads.fixture.test/allowed.js\n##.banner\n") {
		t.Fatalf("manifest lacks proof of the shipped list: %+v", manifest)
	}
	if !blocker.match(thirdPartyScript("https://ads.fixture.test/x.js"), "s1").Blocked {
		t.Fatal("installed list does not block")
	}
	if blocker.match(thirdPartyScript("https://ads.fixture.test/allowed.js"), "s1").Blocked {
		t.Fatal("exception rule ignored")
	}
	restored, err := NewAdBlockerWithConfig(nil, cfg.RolloutConfig())
	if err != nil {
		t.Fatal(err)
	}
	cached, err := NewEasyListUpdater(cfg, restored)
	if err != nil {
		t.Fatal(err)
	}
	if err := cached.LoadCached(); err != nil || cached.Current() == nil || cached.Current().SHA256 != manifest.SHA256 {
		t.Fatalf("cached version not restored: %+v %v", cached.Current(), err)
	}
	if !restored.match(thirdPartyScript("https://ads.fixture.test/x.js"), "s1").Blocked {
		t.Fatal("restored list does not block")
	}
	path := filepath.Join(cfg.StorageDir, manifest.ID, feed.File)
	if err := os.WriteFile(path, []byte("||everything.test^\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tampered, err := NewEasyListUpdater(cfg, NewAdBlocker())
	if err != nil {
		t.Fatal(err)
	}
	if err := tampered.LoadCached(); err == nil || !strings.Contains(err.Error(), "checksum mismatch") || tampered.Current() != nil {
		t.Fatalf("tampered cache was installed: %v", err)
	}
}

func TestEasyListUpdateRejectsUnpinnedMismatchedAndOversizedLists(t *testing.T) {
	lists, cfg := newListFixture(t)
	publishList(lists, "||one.test^\n")
	blocker := NewAdBlocker()
	unpinned := cfg
	unpinned.Pins = []string{"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}
	updater, err := NewEasyListUpdater(unpinned, blocker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updater.Update(t.Context()); !errors.Is(err, ErrEasyListPin) {
		t.Fatalf("unpinned source accepted: %v", err)
	}
	updater, err = NewEasyListUpdater(cfg, blocker)
	if err != nil {
		t.Fatal(err)
	}
	lists.set("/mirror.txt", "! Version: 2\n||other.test^\n")
	if _, err := updater.Update(t.Context()); err == nil || !strings.Contains(err.Error(), "checksums differ") {
		t.Fatalf("mirror mismatch accepted: %v", err)
	}
	publishList(lists, "||one.test^\n")
	if _, err := updater.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	publishList(lists, strings.Repeat("||growth.test^\n", 3))
	if _, err := updater.Update(t.Context()); err == nil || !strings.Contains(err.Error(), "exceeds 2.0x") {
		t.Fatalf("oversized list accepted: %v", err)
	}
	if !blocker.match(thirdPartyScript("https://one.test/a.js"), "s").Blocked || blocker.match(thirdPartyScript("https://growth.test/a.js"), "s").Blocked {
		t.Fatal("a rejected update replaced the cached list")
	}
	for _, feed := range []EasyListFeed{{Name: "x", Primary: "http://a.test/l", Mirror: "https://b.test/l"}, {Name: "x", Primary: "https://a.test/l", Mirror: "https://a.test/l"}} {
		bad := cfg
		bad.Feeds = []EasyListFeed{feed}
		if _, err := NewEasyListUpdater(bad, blocker); err == nil {
			t.Fatalf("feed %+v accepted", feed)
		}
	}
}

func TestEasyListRetriesFailedUpdatesHourlyNotEveryTick(t *testing.T) {
	lists, cfg := newListFixture(t)
	updater, err := NewEasyListUpdater(cfg, NewAdBlocker())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	updater.now = func() time.Time { return now }
	if _, err := updater.UpdateIfDue(t.Context()); err == nil {
		t.Fatal("missing list did not fail")
	}
	now = now.Add(30 * time.Minute)
	if _, err := updater.UpdateIfDue(t.Context()); err != nil {
		t.Fatalf("retry happened before the retry interval: %v", err)
	}
	if lists.hits["/primary.txt"] != 1 {
		t.Fatalf("source hammered: %d", lists.hits["/primary.txt"])
	}
	publishList(lists, "||late.test^\n")
	now = now.Add(31 * time.Minute)
	if manifest, err := updater.UpdateIfDue(t.Context()); err != nil || manifest == nil {
		t.Fatalf("retry after interval failed: %v", err)
	}
}

func TestAdBlockerGradualRolloutPromotesOrRollsBack(t *testing.T) {
	lists, cfg := newListFixture(t)
	publishList(lists, "||first.test^\n")
	blocker, err := NewAdBlockerWithConfig(nil, cfg.RolloutConfig())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	blocker.now = func() time.Time { return now }
	updater, err := NewEasyListUpdater(cfg, blocker)
	if err != nil {
		t.Fatal(err)
	}
	updater.now = blocker.now
	if _, err := updater.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	inBucket, outBucket := "", ""
	for i := 0; inBucket == "" || outBucket == ""; i++ {
		id := "session-" + strings.Repeat("x", i%7) + string(rune('a'+i%26))
		if rolloutBucket(id) < 50 {
			inBucket = id
		} else {
			outBucket = id
		}
	}
	now = now.Add(time.Minute)
	publishList(lists, "||second.test^\n")
	second, err := updater.Update(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !blocker.match(thirdPartyScript("https://second.test/a.js"), inBucket).Blocked || blocker.match(thirdPartyScript("https://second.test/a.js"), outBucket).Blocked {
		t.Fatal("candidate did not reach exactly the rollout bucket")
	}
	if stable, candidate := blocker.Advance(); stable == nil || candidate == nil || candidate.SHA256 != second.SHA256 {
		t.Fatal("candidate promoted before the rollout delay")
	}
	for i := 0; i < 5; i++ {
		if blocker.RecordBreakage() {
			t.Fatal("rollback at or below the breakage threshold")
		}
	}
	if !blocker.RecordBreakage() || blocker.match(thirdPartyScript("https://second.test/a.js"), inBucket).Blocked || !blocker.IsRejected(second.SHA256) {
		t.Fatal("more than five breakages did not roll the candidate back")
	}
	if err := blocker.Install(&AdBlockList{Manifest: *second}); err == nil {
		t.Fatal("rolled-back version was reinstalled")
	}
	now = now.Add(time.Minute)
	publishList(lists, "||third.test^\n")
	if _, err := updater.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if stable, candidate := blocker.Advance(); stable == nil || candidate != nil || !blocker.match(thirdPartyScript("https://third.test/a.js"), outBucket).Blocked {
		t.Fatal("candidate without breakage was not promoted after the delay")
	}
}

func TestEasyListStoreKeepsThreeVersions(t *testing.T) {
	lists, cfg := newListFixture(t)
	updater, err := NewEasyListUpdater(cfg, NewAdBlocker())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	updater.now = func() time.Time { return now }
	for i, host := range []string{"a.test", "b.test", "c.test", "d.test"} {
		now = now.Add(time.Duration(i+1) * time.Minute)
		publishList(lists, "||"+host+"^\n")
		if _, err := updater.Update(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	store, err := updater.readStore()
	if err != nil || len(store.Versions) != 3 {
		t.Fatalf("store keeps %d versions: %v", len(store.Versions), err)
	}
	entries, err := os.ReadDir(cfg.StorageDir)
	if err != nil {
		t.Fatal(err)
	}
	directories := 0
	for _, entry := range entries {
		if entry.IsDir() {
			directories++
		}
	}
	if directories != 3 {
		t.Fatalf("pruned version directories remain: %d", directories)
	}
}

func TestEasyListRestartResumesUnprovenVersionAsCandidate(t *testing.T) {
	lists, cfg := newListFixture(t)
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	first, err := NewEasyListUpdater(cfg, NewAdBlocker())
	if err != nil {
		t.Fatal(err)
	}
	first.now = clock
	publishList(lists, "||old.test^\n")
	if _, err := first.Update(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	publishList(lists, "||new.test^\n")
	newest, err := first.Update(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Minute)
	restarted, err := NewAdBlockerWithConfig(nil, cfg.RolloutConfig())
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = clock
	cached, err := NewEasyListUpdater(cfg, restarted)
	if err != nil {
		t.Fatal(err)
	}
	cached.now = clock
	if err := cached.LoadCached(); err != nil {
		t.Fatal(err)
	}
	stable, candidate := restarted.Advance()
	if stable == nil || candidate == nil || candidate.SHA256 != newest.SHA256 || cached.Current().SHA256 != newest.SHA256 {
		t.Fatalf("restart promoted an unproven version: stable=%+v candidate=%+v", stable, candidate)
	}
	now = now.Add(51 * time.Minute)
	if _, candidate := restarted.Advance(); candidate != nil {
		t.Fatal("resumed candidate not promoted once its original rollout delay passed")
	}
}
