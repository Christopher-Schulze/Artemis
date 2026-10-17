package fixture

import (
	"fmt"
	"net/http"
)

func cssScenarios() []Scenario {
	return []Scenario{
		{
			ID:          "css-001",
			Path:        "/css-001",
			Kind:        KindCSS,
			Description: "Page with an external stylesheet",
			HTML: `<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><title>CSS Fixture</title>
<link rel="stylesheet" href="/css-001-style">
</head><body>
<h1 id="styled">CSS fixture</h1>
<p class="muted">Muted paragraph</p>
</body></html>`,
			Expect: Expect{
				Status:   200,
				Title:    "CSS Fixture",
				Contains: []string{"CSS fixture", "Muted paragraph"},
			},
		},
		{
			ID:          "css-001-style",
			Path:        "/css-001-style",
			Kind:        KindCSS,
			Description: "External stylesheet for css-001",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/css; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				if _, err := fmt.Fprint(w, "#styled { color: red; } .muted { color: gray; } body { font-family: sans-serif; }"); err != nil {
					return
				}
			}),
			Expect: Expect{
				Status:   200,
				Contains: []string{"body", "#styled"},
			},
		},
		{
			ID: "css-002", Path: "/css-002", Kind: KindCSS,
			Description: "Computed inheritance, custom scope, currentColor and box cascade",
			HTML: `<!doctype html><title>CSS Computed Contract</title><style>
html{font-size:16px;color:black}#parent{font-size:20px;--base:red;--alias:var(--base)}
#target{font-size:150%;--base:blue;color:var(--alias);background-color:currentColor;padding:1px 2px;padding-left:3px;opacity:0.123456789}
</style><div id="parent"><div id="target">Computed fixture</div></div>`,
			RunScripts: true,
			Expect: Expect{Status: 200, Title: "CSS Computed Contract", Contains: []string{"Computed fixture"},
				Eval:         `(()=>{const s=getComputedStyle(document.getElementById('target'));const checks=[s.color==='rgb(255, 0, 0)',s.fontSize==='30px',s.backgroundColor==='rgb(255, 0, 0)',s.padding==='1px 2px 1px 3px',s.opacity==='0.123457',s.getPropertyValue('--alias').trim()==='red',s.getPropertyValue('--base').trim()==='blue'];return checks.every(Boolean)?'css-002:pass':JSON.stringify(checks);})()`,
				EvalContains: "css-002:pass"},
		},
		{
			ID: "css-003", Path: "/css-003", Kind: KindCSS,
			Description: "Live ordered inline CSSOM, priority mutation and computed read-only ownership",
			HTML:        `<!doctype html><title>CSSOM Contract</title><div id="target" style="padding:2px;color:red!important">CSSOM fixture</div>`,
			RunScripts:  true,
			Expect: Expect{Status: 200, Title: "CSSOM Contract", Contains: []string{"CSSOM fixture"},
				Eval:         `(()=>{const n=document.getElementById('target'),s=n.style,checks=[s===n.style,s.color==='red',s.getPropertyPriority('color')==='important'];s.paddingLeft='3px';s.setProperty('color','navy','IMPORTANT');const computed=getComputedStyle(n);checks.push(s.padding==='2px 2px 2px 3px',computed.color==='rgb(0, 0, 128)');let refused=false;try{computed.color='red'}catch(e){refused=e.name==='NoModificationAllowedError'&&e.code===7}checks.push(refused,s.removeProperty('padding')==='',s.length===1,s.item(0)==='color',s.cssText==='color: navy !important;');return checks.every(Boolean)?'css-003:pass':JSON.stringify(checks);})()`,
				EvalContains: "css-003:pass"},
		},
		{
			ID: "css-004", Path: "/css-004", Kind: KindCSS,
			Description: "Quoted declaration boundaries, priority and whitespace-sensitive empty selector",
			HTML: `<!doctype html><title>CSS Token Contract</title><style>
#target{--text:"a;b}c";color:red!important;color:blue}#space{color:blue}#space:empty{color:red}
</style><div id="target">Token fixture</div><div id="space"> </div>`,
			RunScripts: true,
			Expect: Expect{Status: 200, Title: "CSS Token Contract", Contains: []string{"Token fixture"},
				Eval:         `(()=>{const s=getComputedStyle(document.getElementById('target'));const checks=[s.getPropertyValue('--text').trim()==='"a;b}c"',s.color==='rgb(255, 0, 0)',getComputedStyle(document.getElementById('space')).color==='rgb(0, 0, 255)'];return checks.every(Boolean)?'css-004:pass':JSON.stringify(checks);})()`,
				EvalContains: "css-004:pass"},
		},
	}
}
