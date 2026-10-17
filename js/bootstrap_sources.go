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

// inlineGetComputedStyleBootstrap installs the JS-side wrapper for
// getComputedStyle, factored out of installStyleManagerBridge. Backed
// by the __cascade_style native callback (looked up lazily at call
// time, so safe to bake into a snapshot).
const inlineGetComputedStyleBootstrap = `
(() => {
  globalThis.getComputedStyle = function(el, pseudo = '') {
    if (!el || !el.__id || el.nodeType !== 1) throw new TypeError('getComputedStyle requires an Element');
    const id = el.__id;
    if (pseudo !== null && String(pseudo) !== '') __cascade_style(id, '__unsupported_pseudo_style__');
    const readOnly = () => { throw new DOMException('Computed style is read only', 'NoModificationAllowedError'); };
    const target = {
      getPropertyValue(property) {
        if (!arguments.length) throw new TypeError('getPropertyValue requires a property');
        if (typeof property === 'symbol') throw new TypeError('CSSOM requires a DOMString');
        return __cascade_style(id, String(property));
      },
      getPropertyPriority(property) {
        if (!arguments.length) throw new TypeError('getPropertyPriority requires a property');
        if (typeof property === 'symbol') throw new TypeError('CSSOM requires a DOMString');
        return '';
      },
      setProperty: readOnly,
      removeProperty: readOnly,
      get cssText() { return ''; },
      set cssText(value) { readOnly(); },
      get parentRule() { return null; },
      [Symbol.toStringTag]: 'CSSStyleDeclaration',
    };
    return new Proxy(target, {
      get(target, prop, receiver) {
        if (Reflect.has(target, prop)) return Reflect.get(target, prop, receiver);
        if (typeof prop !== 'string') return undefined;
        return __cascade_style(id, prop);
      },
      set: readOnly,
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
// Tainted bootstraps -- see taintedBootstrapNames -- chain dispatch
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
		{"artemis-parity-gaps", parityGapsBootstrap},
		{"artemis-pool-reset", poolResetBootstrap},
	}
}

// taintedBootstrapNames identifies bootstraps that must not be baked
// into the snapshot (they capture cross-bootstrap functions in
// closures). flushBootstraps in NewContext runs these per-Context even
// when a snapshot is loaded.
var taintedBootstrapNames = map[string]bool{
	"artemis-fetch-response":    true,
	"artemis-websocket":         true,
	"artemis-timers":            true,
	"artemis-extras-v2-onattrs": true,
}
