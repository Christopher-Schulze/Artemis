package js

import (
	"context"
	"testing"
)

func TestNavigatorUAData(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	v, err := c.Eval(context.Background(), `
		(() => {
			const ua = navigator.userAgentData;
			return ua.brands.length + ':' + (typeof ua.getHighEntropyValues);
		})()
	`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "3:function" {
		t.Errorf("got %q", v.String())
	}
}

func TestClipboardWriteRead(t *testing.T) {
	c := newCtxFromHTML(t, `<html></html>`, nil)
	if _, err := c.Eval(context.Background(), `
		var captured = '';
		(async () => {
			await navigator.clipboard.writeText('hi');
			captured = await navigator.clipboard.readText();
		})();
	`); err != nil {
		t.Fatalf("eval: %v", err)
	}
	v, _ := c.Eval(context.Background(), `captured`)
	if v.String() != "hi" {
		t.Errorf("got %q", v.String())
	}
}
