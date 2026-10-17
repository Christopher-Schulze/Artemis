package css

import (
	"strings"
	"testing"
)

func TestCSSCascadeAllStaticOriginsAndImportance(t *testing.T) {
	// Ascending cascade order, intentionally supplied in reverse source order.
	tiers := []struct {
		origin    Origin
		important bool
		value     string
	}{
		{OriginUserAgent, false, "ua-normal"},
		{OriginUser, false, "user-normal"},
		{OriginAuthor, false, "author-normal"},
		{OriginAuthor, true, "author-important"},
		{OriginUser, true, "user-important"},
		{OriginUserAgent, true, "ua-important"},
	}
	node := parseAndFind(t, `<div id="x" class="a"></div>`, "#x")
	for tier := range tiers {
		var sheets []*Stylesheet
		for index := tier; index >= 0; index-- {
			priority := ""
			if tiers[index].important {
				priority = " !important"
			}
			sheet := ParseStylesheet(".a { --value: " + tiers[index].value + priority + " }")
			sheet.Origin = tiers[index].origin
			sheets = append(sheets, sheet)
		}
		candidates, err := collectCSSCandidates(sheets, node, "")
		if err != nil || len(candidates["--value"]) == 0 || candidates["--value"][0].declaration.Value != tiers[tier].value {
			t.Fatalf("cascade origin tier %d failed: %v %v", tier, candidates, err)
		}
	}
}

func TestCSSCascadeUsesSpecificityTuplesAndOrderedDeclarations(t *testing.T) {
	node := parseAndFind(t, `<div id="x" class="a"></div>`, "#x")
	for _, test := range []struct {
		name   string
		source string
		inline string
		want   string
	}{
		{"tuple", `#x { --v: id } ` + strings.Repeat(".a", 12) + ` { --v: classes }`, "", "id"},
		{"selector list", `#x, .a { --v: list } .a.a { --v: compound }`, "", "list"},
		{"declaration priority", `.a { --v: important !important; --v: normal }`, "", "important"},
		{"inline normal", `#x { --v: sheet }`, "--v: inline", "inline"},
		{"sheet important", `#x { --v: sheet !important }`, "--v: inline", "sheet"},
		{"inline important", `#x { --v: sheet !important }`, "--v: inline !important; --v: normal", "inline"},
		{"source order", `.a { --v: first; --v: second } .a { --v: third }`, "", "third"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidates, err := collectCSSCandidates([]*Stylesheet{ParseStylesheet(test.source)}, node, test.inline)
			if err != nil || len(candidates["--v"]) == 0 || candidates["--v"][0].declaration.Value != test.want {
				t.Fatalf("ordered cascade failed: %v %v", candidates, err)
			}
		})
	}
}

func TestCSSCascadeRollbackRemovesWholeOriginOrInlineAttachment(t *testing.T) {
	node := parseAndFind(t, `<div id="x"></div>`, "#x")
	for _, test := range []struct {
		name   string
		author string
		user   string
		inline string
		want   string
	}{
		{"author revert", "red", "green", "revert", "green"},
		{"user important revert", "red !important", "revert !important", "blue !important", "ua"},
		{"inline layer", "red", "green", "revert-layer", "red"},
		{"inline important layer", "red !important", "green", "revert-layer !important", "red"},
		{"unlayered origin", "revert-layer !important", "green", "", "green"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ua := ParseStylesheet("#x { --v: ua }")
			ua.Origin = OriginUserAgent
			user := ParseStylesheet("#x { --v: " + test.user + " }")
			user.Origin = OriginUser
			author := ParseStylesheet("#x { --v: old; --v: " + test.author + " }")
			inline := ""
			if test.inline != "" {
				inline = "--v: " + test.inline
			}
			candidates, err := collectCSSCandidates([]*Stylesheet{ua, user, author}, node, inline)
			if err != nil {
				t.Fatal(err)
			}
			remaining := resolveCSSRollbacks(candidates["--v"])
			if len(remaining) == 0 || remaining[0].declaration.Value != test.want {
				t.Fatalf("rollback resurrected wrong origin/declaration: %v", remaining)
			}
		})
	}
}
