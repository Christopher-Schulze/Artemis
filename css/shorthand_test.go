package css

import "testing"

func TestCSSShorthandsPreserveLonghandOrderPriorityAndPendingVariables(t *testing.T) {
	node := parseAndFind(t, `<div id="x"></div>`, "#x")
	for _, test := range []struct{ inline, property, want string }{
		{"padding:1px 2px", "padding", "1px 2px"},
		{"padding:1px 2px 3px", "padding", "1px 2px 3px"},
		{"padding:1px 2px 3px 4px;padding-left:9px;padding-right:7px !important;padding:2px", "padding", "2px 7px 2px 2px"},
		{"--space:1px 2px 3px 4px;padding:var(--space);padding-left:8px", "padding", "1px 2px 3px 8px"},
		{"padding:5px;padding:var(--missing);padding-left:8px", "padding", "0px 0px 0px 8px"},
		{"--space:-1px 2px 3px 4px;padding:5px;padding:var(--space)", "padding", "0px"},
		{"margin:-1px 2px 3px 4px;margin-left:8px", "margin", "-1px 2px 3px 8px"},
		{"background:blue;background-color:red", "background-color", "rgb(255, 0, 0)"},
		{"background-color:red;background:blue", "background-color", "rgb(0, 0, 255)"},
		{"padding:initial", "padding", "0px"},
	} {
		style := requireCascade(t, nil, node, test.inline)
		actual, err := style.Value(test.property)
		if err != nil || actual != test.want {
			t.Fatalf("inline=%q property=%s actual=%q expected=%q err=%v", test.inline, test.property, actual, test.want, err)
		}
	}
}

func TestCSSShorthandsDoNotFabricateLayoutOrUnspecifiedUserAgentValues(t *testing.T) {
	node := parseAndFind(t, `<div id="x"></div>`, "#x")
	for _, test := range []struct{ inline, property string }{
		{"padding:50% 4px", "padding-right"}, {"margin:auto", "margin"},
		{"padding:0.1px", "padding"}, {"background:red url(image.png)", "background"},
		{"", "margin"}, {"", "padding"}, {"", "background-color"},
	} {
		style := requireCascade(t, nil, node, test.inline)
		if value, err := style.Value(test.property); value != "" || !IsDiagnostic(err) {
			t.Fatalf("unproven shorthand/layout/UA semantics acquired a value: %s=%q err=%v", test.property, value, err)
		}
	}
}
