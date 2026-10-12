package fixture

func htmlScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "html-001",
			Path:        "/html-001",
			Kind:        KindHTML,
			Description: "Simple HTML page with title, headings, paragraphs, and links",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>HTML Fixture</title>
</head><body>
<header><h1>HTML Fixture</h1></header>
<main><p>This is a simple HTML fixture.</p><p>It has <a href="/html-001/sub">two</a> <a href="/html-001/other">links</a>.</p></main>
<footer><p>(c) 2026 Fixture</p></footer>
</body></html>`,
			Expect: Expect{
				Status:   200,
				Title:    "HTML Fixture",
				Contains: []string{"HTML Fixture", "simple HTML fixture", "two"},
			},
		},
	}
}

func navigationScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "nav-001",
			Path:        "/nav-001",
			Kind:        KindNavigation,
			Description: "Navigation page with links",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>Navigation</title>
</head><body>
<nav><a href="/page-1">Page 1</a> | <a href="/page-2">Page 2</a> | <a href="/page-3">Page 3</a></nav>
<main><h1>Navigation Fixture</h1><p>Navigate the fixture server.</p></main>
</body></html>`,
			Expect: Expect{
				Status:   200,
				Title:    "Navigation",
				Contains: []string{"Navigation Fixture", "Page 1", "Page 2"},
			},
		},
	}
}
