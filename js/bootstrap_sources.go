package js

// BootstrapSource is one named JS bootstrap snippet that NewContext
// evaluates after binding native callbacks. Exposed so the offline
// snapshot tool can bake the same set into a V8 startup snapshot.
//
// The order matches NewContext's install* call order and is significant:
// later sources may reference globals or prototypes installed by earlier
// ones (notably ELEM_PROTO from dom_bridge).
type BootstrapSource struct {
	Name   string
	Source string
}

// inlineWindowEventsBootstrap is the window EventTarget plumbing
// (addEventListener / removeEventListener / dispatchEvent) factored out
// of installWindow so the snapshot tool can include it.
const inlineWindowEventsBootstrap = `
(() => {
  const _winListeners = new Map();
  window.addEventListener = function(type, fn) {
    if (typeof fn !== 'function') return;
    let s = _winListeners.get(type);
    if (!s) { s = new Set(); _winListeners.set(type, s); }
    s.add(fn);
  };
  window.removeEventListener = function(type, fn) {
    const s = _winListeners.get(type);
    if (s) s.delete(fn);
  };
  window.dispatchEvent = function(ev) {
    if (!ev || !ev.type) return false;
    const s = _winListeners.get(ev.type);
    if (s) for (const fn of s) { try { fn(ev); } catch (e) {} }
    return !ev.defaultPrevented;
  };
})();
`

// inlineGetComputedStyleBootstrap installs the JS-side wrapper for
// getComputedStyle, factored out of installStyleManagerBridge. Backed
// by the __cascade_style native callback (looked up lazily at call
// time, so safe to bake into a snapshot).
const inlineGetComputedStyleBootstrap = `
(() => {
  globalThis.getComputedStyle = function(el) {
    if (!el || !el.__id) return {};
    const id = el.__id;
    return new Proxy({}, {
      get(_t, prop) {
        if (typeof prop !== 'string') return undefined;
        return __cascade_style(id, prop);
      },
    });
  };
})();
`

// BootstrapSources returns the leaf (snapshot-eligible) bootstrap
// sources: pure-JS class/prototype/constant definitions whose top-level
// code does not capture other bootstrap-installed functions into
// closures. These are safe to bake into a V8 startup snapshot because
// any native callback they reference resolves lazily at call time.
//
// Tainted bootstraps -- see TaintedBootstrapSources -- chain dispatch
// across earlier bootstraps via closure capture (e.g. const _impPrev =
// crypto.subtle.importKey). Capturing at snapshot time would freeze
// undefined values, so those run per-Context at NewContext time.
func BootstrapSources() []BootstrapSource {
	return []BootstrapSource{
		{"artemis-dom-bridge", domBootstrap},
		{"artemis-extras", extrasBootstrap},
		{"artemis-extras-v2", extrasV2Bootstrap},
		{"artemis-mutation-observer", mutationObserverBootstrap},
		{"artemis-webapi-more", webapiMoreBootstrap},
		{"artemis-webapi-v3", webapiV3Bootstrap},
		{"artemis-iframe", iframeBootstrap},
		{"artemis-input-props", inputPropsBootstrap},
		{"artemis-navigator-extras", navigatorExtrasBootstrap},
		{"artemis-style-manager", inlineGetComputedStyleBootstrap},
		{"artemis-websocket", websocketBootstrap},
		{"artemis-window-events", inlineWindowEventsBootstrap},
		{"artemis-parity-gaps", parityGapsBootstrap},
		{"artemis-pool-reset", poolResetBootstrap},
	}
}

// taintedBootstrapNames identifies bootstraps that must not be baked
// into the snapshot (they capture cross-bootstrap functions in
// closures). flushBootstraps in NewContext runs these per-Context even
// when a snapshot is loaded.
var taintedBootstrapNames = map[string]bool{
	"artemis-crypto-aes":        true,
	"artemis-crypto-asym":       true,
	"artemis-crypto-complete":   true,
	"artemis-crypto-extra":      true,
	"artemis-crypto-pkcs8":      true,
	"artemis-extras-v2-onattrs": true,
}
