package js

import (
	"errors"
	"net/http"
	standard "net/http/cookiejar"
	"testing"

	"github.com/Christopher-Schulze/Artemis/network/cookiejar"
)

func TestDocumentCookiesRejectOpaqueAndNilCookieAuthority(t *testing.T) {
	opaque, err := standard.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	var typedNil *cookiejar.Jar
	for _, jar := range []http.CookieJar{opaque, typedNil} {
		runtime := NewRuntime()
		c, err := runtime.NewContext(mustParse(t, "<html></html>", "https://example.test/"), ContextOpts{CookieJar: jar})
		if c != nil || !errors.Is(err, ErrDocumentCookieAuthority) {
			runtime.Close()
			t.Fatalf("opaque/nil HTTP jar became document authority: context=%v err=%v", c, err)
		}
		count := 0
		runtime.contextRegistry.Range(func(_, _ any) bool { count++; return true })
		if count != 0 {
			runtime.Close()
			t.Fatal("failed cookie admission retained a context registration")
		}
		valid, err := runtime.NewContext(mustParse(t, "<html></html>", "https://example.test/"), ContextOpts{})
		if err != nil {
			runtime.Close()
			t.Fatal(err)
		}
		assertExecutionRecovery(t, valid)
		valid.Close()
		runtime.Close()
	}
}
