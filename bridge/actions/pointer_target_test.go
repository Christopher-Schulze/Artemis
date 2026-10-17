package actions

import (
	"math"
	"strings"
	"testing"
)

func TestValidateRequestBoundsPointerTargets(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request Request
		wantErr string
	}{
		{"click point", Request{Kind: KindClick, Pointer: &PointerTarget{X: 10, Y: 20}}, ""},
		{"hover point", Request{Kind: KindHover, Pointer: &PointerTarget{X: 0, Y: 0}}, ""},
		{"fill point", Request{Kind: KindFill, Text: "x", Pointer: &PointerTarget{X: 1, Y: 1}}, "only to click or hover"},
		{"point with ref", Request{Kind: KindClick, Ref: "e1", Pointer: &PointerTarget{X: 1, Y: 1}}, "without references"},
		{"negative point", Request{Kind: KindClick, Pointer: &PointerTarget{X: -1, Y: 1}}, "finite viewport point"},
		{"nan point", Request{Kind: KindClick, Pointer: &PointerTarget{X: math.NaN(), Y: 1}}, "finite viewport point"},
		{"click without target", Request{Kind: KindClick}, "stable ref required"},
	} {
		err := ValidateRequest(tc.request)
		if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
			t.Fatalf("%s: err = %v, want %q", tc.name, err, tc.wantErr)
		}
	}
}
