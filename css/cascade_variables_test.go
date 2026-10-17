package css

import "testing"

func TestCSSCascadeVariablesUseComputedParentAndDoNotResurrectInvalidValues(t *testing.T) {
	sheets := []*Stylesheet{ParseStylesheet(`body { --base: red; --alias: var(--base) } #x { --base: blue; --alias: older; --alias: var(--missing); --use: var(--alias, green); color: red; color: var(--missing) }`)}
	node := parseAndFind(t, `<body id="parent"><div id="x"></div></body>`, "#x")
	parent := node.Parent
	parentStyle, err := cascadeCSSNode(sheets, parent, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	style, err := cascadeCSSNode(sheets, node, "", parentStyle.variables)
	if err != nil {
		t.Fatal(err)
	}
	if cssTokenText(style.variables["--base"]) != "blue" || cssTokenText(style.variables["--use"]) != "green" || style.winners["color"].declaration.Value != "var(--missing)" {
		t.Fatalf("custom scope or invalid-at-computed cascade changed: %v", style)
	}
	if _, valid := style.variables["--alias"]; valid {
		t.Fatal("invalid winner resurrected an earlier declaration or inherited value")
	}
	if cssTokenText(parentStyle.variables["--alias"]) != "red" {
		t.Fatal("child resolution changed the computed parent")
	}
}

func TestCSSCascadeGeneratedVariableRollbackPreservesOriginAndDependents(t *testing.T) {
	node := parseAndFind(t, `<div id="x"></div>`, "#x")
	user := ParseStylesheet(`#x { --v: green }`)
	user.Origin = OriginUser
	author := ParseStylesheet(`#x { --v: red; --dependent: var(--v, wrong) }`)
	for _, test := range []struct {
		inline string
		want   string
	}{
		{"--v: var(--missing, revert)", "green"},
		{"--v: var(--missing, revert-layer)", "red"},
		{"--v: var(--missing, revert-layer) !important", "red"},
	} {
		style, err := cascadeCSSNode([]*Stylesheet{user, author}, node, test.inline, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cssTokenText(style.variables["--v"]) != test.want || cssTokenText(style.variables["--dependent"]) != test.want {
			t.Fatalf("generated origin rollback or dependent fallback failed for %q: %v", test.inline, style)
		}
	}
}

func TestCSSCascadeOrdinaryVariableFailureAndGeneratedRollback(t *testing.T) {
	node := parseAndFind(t, `<div id="x"></div>`, "#x")
	user := ParseStylesheet(`#x { color: green }`)
	user.Origin = OriginUser
	for _, test := range []struct {
		inline string
		want   string
		valid  bool
	}{
		{"color: red; color: var(--missing)", "", false},
		{"color: var(--missing, revert)", "green", true},
		{"color: var(--missing, revert-layer)", "red", true},
	} {
		style, err := cascadeCSSNode([]*Stylesheet{user, ParseStylesheet(`#x {color: red}`)}, node, test.inline, nil)
		if err != nil {
			t.Fatal(err)
		}
		value, err := style.resolveProperty("color")
		if err != nil || value.valid != test.valid || cssTokenText(value.tokens) != test.want {
			t.Fatalf("ordinary computed failure resurrected an earlier declaration or lost rollback: value=%q valid=%v err=%v", cssTokenText(value.tokens), value.valid, err)
		}
	}
}

func TestCSSComputedRollbackUsesEffectiveDeclarationProvenance(t *testing.T) {
	node := parseAndFind(t, `<div id="x"></div>`, "#x")
	user := ParseStylesheet(`#x{color:#12}`)
	user.Origin = OriginUser
	author := ParseStylesheet(`html{color:red}#x{color:var(--missing,revert)}`)
	style, err := Cascade([]*Stylesheet{user, author}, node, "")
	if err != nil {
		t.Fatal(err)
	}
	if value, err := style.Value("color"); value != "" || !IsDiagnostic(err) {
		t.Fatalf("earlier variable winner laundered a later literal parse failure into inheritance: %q %v", value, err)
	}
}
