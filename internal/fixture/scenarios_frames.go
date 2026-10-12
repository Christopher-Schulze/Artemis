package fixture

func frameScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "frame-001",
			Path:        "/frame-001",
			Kind:        KindFrame,
			Description: "Page with an iframe",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Frame Fixture</title>
</head><body>
<h1>Outer frame</h1>
<iframe id="f" src="/frame-001-inner"></iframe>
</body></html>`,
			Expect: Expect{
				Status:   200,
				Title:    "Frame Fixture",
				Contains: []string{"Outer frame"},
				Eval: `(function() {
					var f = document.getElementById('f');
					var d = f.contentDocument;
					if (!d) return 'no doc';
					var h = d.getElementById('inner-h');
					return h ? h.textContent : 'no inner-h';
				})()`,
				EvalContains: "Inner heading",
			},
		},
		{
			ID:          "frame-001-inner",
			Path:        "/frame-001-inner",
			Kind:        KindFrame,
			Description: "Inner iframe document",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Inner</title>
</head><body>
<h1 id="inner-h">Inner heading</h1>
<p>Inner paragraph</p>
</body></html>`,
			Expect: Expect{
				Status:   200,
				Contains: []string{"Inner heading", "Inner paragraph"},
			},
		},
	}
}

func shadowScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "shadow-001",
			Path:        "/shadow-001",
			Kind:        KindShadowDOM,
			Description: "Shadow DOM attachShadow fixture",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Shadow DOM</title>
</head><body>
<div id="host"></div>
<script>
  var host = document.getElementById('host');
  if (typeof host.attachShadow === 'function') {
    host.attachShadow({mode: 'open'});
    window.shadowOK = 'ok';
  } else {
    window.shadowOK = 'missing';
  }
</script>
</body></html>`,
			RunScripts: true,
			Expect: Expect{
				Status:       200,
				Title:        "Shadow DOM",
				Eval:         "window.shadowOK",
				EvalContains: "ok",
			},
		},
	}
}

func dialogScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "dialog-001",
			Path:        "/dialog-001",
			Kind:        KindDialog,
			Description: "HTML dialog element fixture",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Dialog</title>
</head><body>
<dialog id="d"><p>Hello dialog</p></dialog>
<script>
  var d = document.getElementById('d');
  window.dialogOK = (d && d.tagName.toLowerCase() === 'dialog') ? 'ok' : 'missing';
</script>
</body></html>`,
			RunScripts: true,
			Expect: Expect{
				Status:       200,
				Title:        "Dialog",
				Eval:         "window.dialogOK",
				EvalContains: "ok",
			},
		},
	}
}

func canvasScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "canvas-001",
			Path:        "/canvas-001",
			Kind:        KindCanvas,
			Description: "Canvas 2D context fixture",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Canvas</title>
</head><body>
<canvas id="c" width="100" height="100"></canvas>
<script>
  var canvas = document.getElementById('c');
  var ctx = canvas.getContext('2d');
  window.canvasOK = ctx ? 'ok' : 'fail';
  if (ctx) ctx.fillRect(0, 0, 10, 10);
</script>
</body></html>`,
			RunScripts: true,
			Expect: Expect{
				Status:       200,
				Title:        "Canvas",
				Eval:         "window.canvasOK",
				EvalContains: "ok",
			},
		},
	}
}
