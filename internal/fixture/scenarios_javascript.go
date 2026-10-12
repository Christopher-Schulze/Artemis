package fixture

func jsScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "js-001",
			Path:        "/js-001",
			Kind:        KindJS,
			Description: "Inline JavaScript execution fixture",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>JS Fixture</title>
</head><body>
<div id="out">before</div>
<script>
  globalThis.jsTest = 'ok';
  document.getElementById('out').textContent = 'js-ok';
</script>
</body></html>`,
			RunScripts: true,
			Expect: Expect{
				Status:       200,
				Title:        "JS Fixture",
				Contains:     []string{"js-ok"},
				Eval:         "globalThis.jsTest",
				EvalContains: "ok",
			},
		},
	}
}
