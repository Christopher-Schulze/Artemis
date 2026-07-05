package js

import (
	"strings"

	v8 "rogchap.com/v8go"

	"github.com/Christopher-Schulze/Artemis/css"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

// styleManager collects CSS stylesheets that apply to the document:
// inline `<style>` tags AND external `<link rel=stylesheet href=...>`
// fetched via the engine's StylesheetLoader.
type styleManager struct {
	sheets []*css.Stylesheet
}

// StylesheetLoader is what the styleManager uses to fetch external
// stylesheets. The engine provides one wrapping its HTTP client.
type StylesheetLoader func(url string) ([]byte, error)

// newStyleManager harvests every <style> element under the document and
// every <link rel=stylesheet href=...> with the loader. Loader may be
// nil to skip external stylesheets.
func newStyleManager(doc *webapi.Document, loader StylesheetLoader) *styleManager {
	if doc == nil || doc.Root() == nil {
		return &styleManager{}
	}
	var sheets []*css.Stylesheet

	// Inline <style>
	styles, err := doc.QuerySelectorAll("style")
	if err == nil {
		for _, s := range styles {
			text := s.Text()
			if strings.TrimSpace(text) == "" {
				continue
			}
			sheets = append(sheets, css.ParseStylesheet(text))
		}
	}

	// External <link rel=stylesheet>
	if loader != nil {
		links, err := doc.QuerySelectorAll("link")
		if err == nil {
			for _, l := range links {
				rel := strings.ToLower(l.AttrOrEmpty("rel"))
				if !strings.Contains(rel, "stylesheet") {
					continue
				}
				href := l.AttrOrEmpty("href")
				if href == "" {
					continue
				}
				body, err := loader(href)
				if err != nil || len(body) == 0 {
					continue
				}
				sheets = append(sheets, css.ParseStylesheet(string(body)))
			}
		}
	}

	return &styleManager{sheets: sheets}
}

// computed returns the cascaded styles for n, merged with the inline
// style attribute. !important from stylesheets wins over inline; inline
// wins over non-important sheet declarations.
func (m *styleManager) computed(n *webapi.Node) map[string]string {
	if m == nil || n == nil {
		return map[string]string{}
	}
	inline := n.AttrOrEmpty("style")
	return css.Cascade(m.sheets, n.Raw(), inline)
}

func (r *Runtime) ensureCascadeStyleTemplate() *v8.FunctionTemplate {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cascadeStyleTemplate != nil {
		return r.cascadeStyleTemplate
	}
	iso := r.iso
	r.cascadeStyleTemplate = v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		c := r.contextFor(info.Context())
		args := info.Args()
		if c == nil || len(args) < 2 {
			return mustValue(iso, "")
		}
		n := c.nodes.Get(uint32(args[0].Integer()))
		if n == nil {
			return mustValue(iso, "")
		}
		styles := c.styleMgr.computed(n)
		return mustValue(iso, styles[css.CamelToKebab(args[1].String())])
	})
	return r.cascadeStyleTemplate
}

// installStyleManagerBridge exposes a `__cascade_style(id, prop)` native
// trampoline. The JS-side getComputedStyle proxy now consults this
// before falling back to inline style.
func installStyleManagerBridge(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t := c.rt.ensureCascadeStyleTemplate()
	if err := v8ctx.Global().Set("__cascade_style", t.GetFunction(v8ctx)); err != nil {
		return err
	}
	c.registerBootstrap("artemis-style-manager", inlineGetComputedStyleBootstrap)
	return nil
}
