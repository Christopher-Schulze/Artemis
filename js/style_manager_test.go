package js

import (
	"context"
	"testing"
)

func TestGetComputedStyleFromStylesheet(t *testing.T) {
	c := newCtxFromHTML(t, `<html><head>
<style>
  .card { background: blue; padding: 8px; }
  #x { color: red !important; }
  p { color: green; }
</style>
</head><body>
  <p id="x" class="card" style="margin: 4px">hello</p>
</body></html>`, nil)
	cases := []struct {
		expr string
		want string
	}{
		{`getComputedStyle(document.getElementById('x')).color`, "red"},       // !important wins
		{`getComputedStyle(document.getElementById('x')).background`, "blue"}, // class
		{`getComputedStyle(document.getElementById('x')).padding`, "8px"},     // class
		{`getComputedStyle(document.getElementById('x')).margin`, "4px"},      // inline
	}
	for _, tc := range cases {
		v, err := c.Eval(context.Background(), tc.expr)
		if err != nil {
			t.Errorf("%s: %v", tc.expr, err)
			continue
		}
		if v.String() != tc.want {
			t.Errorf("%s = %q, want %q", tc.expr, v.String(), tc.want)
		}
	}
}

func TestGetComputedStyleNoStylesheet(t *testing.T) {
	c := newCtxFromHTML(t, `<html><body><div id="d" style="color: red"></div></body></html>`, nil)
	v, err := c.Eval(context.Background(), `getComputedStyle(document.getElementById('d')).color`)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v.String() != "red" {
		t.Errorf("got %q", v.String())
	}
}
