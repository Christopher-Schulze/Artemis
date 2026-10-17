package js

import (
	"context"
	"fmt"
	"testing"
)

func TestSessionOriginStorageAndPageIsolation(t *testing.T) {
	runtime := NewRuntime()
	t.Cleanup(runtime.Close)
	open := func(owner, url string) *Context {
		t.Helper()
		c, err := runtime.NewContext(mustParse(t, "<html></html>", url), ContextOpts{SessionID: owner})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(c.Close)
		return c
	}
	seed := open("alice", "https://EXAMPLE.test:443/first")
	if _, err := seed.Eval(context.Background(), "localStorage.setItem('owner','alice'); sessionStorage.setItem('tab','one')"); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ owner, url, want string }{
		{"alice", "https://example.test/next", "alice:null"},
		{"bob", "https://example.test/next", "null:null"},
		{"alice", "http://example.test/next", "null:null"},
		{"alice", "https://sub.example.test/next", "null:null"},
		{"alice", "https://example.test:444/next", "null:null"},
	}
	for _, tc := range cases {
		t.Run(tc.owner+tc.url, func(t *testing.T) {
			c := open(tc.owner, tc.url)
			value, err := c.Eval(context.Background(), "localStorage.getItem('owner')+':'+sessionStorage.getItem('tab')")
			if err != nil {
				t.Fatal(err)
			}
			if value.String() != tc.want {
				t.Fatalf("storage=%q want=%q", value.String(), tc.want)
			}
		})
	}
	runtime.ReleaseSessionStorage("alice")
	value, err := open("alice", "https://example.test/").Eval(context.Background(), "localStorage.getItem('owner')")
	if err != nil {
		t.Fatal(err)
	}
	if !value.IsNull() {
		t.Fatal("released origin retained state")
	}
}

func TestStorageCanonicalOrigins(t *testing.T) {
	cases := []struct {
		first, second string
		same          bool
	}{
		{"https://EXAMPLE.test/a", "https://example.test:443/b", true},
		{"http://example.test/", "http://example.test:80/", true},
		{"https://bücher.test/", "https://xn--bcher-kva.test/", true},
		{"https://[::1]/", "https://[::1]:443/", true},
		{"https://example.test/", "https://example.test:444/", false},
		{"http://example.test/", "https://example.test/", false},
	}
	for _, tc := range cases {
		t.Run(tc.first+tc.second, func(t *testing.T) {
			if (storageOrigin(tc.first) == storageOrigin(tc.second)) != tc.same {
				t.Fatal("origin equivalence mismatch")
			}
		})
	}
}

func TestRuntimeStorageBudgetIsSharedAndReleased(t *testing.T) {
	runtime := NewRuntime()
	defer runtime.Close()
	denied := false
	for i := 0; i < MaxStorageOrigins; i++ {
		c, err := runtime.NewContext(mustParse(t, "<html></html>", fmt.Sprintf("https://budget-%d.test/", i)), ContextOpts{SessionID: "owner"})
		if err != nil {
			t.Fatal(err)
		}
		value, err := c.Eval(context.Background(), fmt.Sprintf("try {localStorage.setItem('payload','x'.repeat(%d));'stored'} catch(e) {e.name}", MaxStorageBytes/2-32))
		if err != nil {
			c.Close()
			t.Fatal(err)
		}
		outcome := value.String()
		c.Close()
		if outcome == "QuotaExceededError" {
			denied = true
			break
		}
		if outcome != "stored" {
			t.Fatalf("unexpected admission=%q", outcome)
		}
	}
	if !denied {
		t.Fatal("aggregate runtime quota not enforced")
	}
	runtime.ReleaseSessionStorage("owner")
	c, err := runtime.NewContext(mustParse(t, "<html></html>", "https://budget-new.test/"), ContextOpts{SessionID: "new-owner"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Eval(context.Background(), "localStorage.setItem('released','yes')"); err != nil {
		t.Fatal(err)
	}
}

func TestStorageQuotaFailsAtomicallyAndCanRecover(t *testing.T) {
	c := newCtxFromHTML(t, "<html></html>", nil)
	value, err := c.Eval(context.Background(), fmt.Sprintf("localStorage.setItem('kept','value');try {localStorage.setItem('oversize','x'.repeat(%d)); 'accepted'} catch(e) {e.name+':'+localStorage.getItem('kept')+':'+localStorage.getItem('oversize')}", MaxStorageBytes))
	if err != nil {
		t.Fatal(err)
	}
	if value.String() != "QuotaExceededError:value:null" {
		t.Fatalf("quota result=%s", value.String())
	}
	value, err = c.Eval(context.Background(), "localStorage.clear(); localStorage.setItem('new','ok'); localStorage.getItem('new')")
	if err != nil {
		t.Fatal(err)
	}
	if value.String() != "ok" {
		t.Fatalf("quota recovery=%s", value.String())
	}
}

func TestStorageOriginAdmissionDoesNotEvictExistingState(t *testing.T) {
	runtime := NewRuntime()
	defer runtime.Close()
	for i := 0; i < MaxStorageOrigins; i++ {
		c, err := runtime.NewContext(mustParse(t, "<html></html>", fmt.Sprintf("https://origin-%d.test/", i)), ContextOpts{SessionID: "owner"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Eval(context.Background(), "localStorage.setItem('retained','yes')"); err != nil {
			t.Fatal(err)
		}
		c.Close()
	}
	if c, err := runtime.NewContext(mustParse(t, "<html></html>", "https://overflow.test/"), ContextOpts{SessionID: "owner"}); err == nil {
		c.Close()
		t.Fatal("origin limit not enforced")
	}
	c, err := runtime.NewContext(mustParse(t, "<html></html>", "https://origin-0.test/"), ContextOpts{SessionID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	value, err := c.Eval(context.Background(), "localStorage.getItem('retained')")
	if err != nil {
		t.Fatal(err)
	}
	if value.String() != "yes" {
		t.Fatal("admission failure evicted existing state")
	}
}
