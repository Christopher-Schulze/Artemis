// artemis-snapshot bakes the static portion of Artemis' JS bootstrap
// into a V8 startup snapshot blob and writes it to js/snapshot.bin.
// The runtime then loads this blob via NewIsolateFromSnapshot, skipping
// the parse + first-evaluation cost of the bootstrap on every NewContext.
//
// What gets baked: every BootstrapSource js.BootstrapSources() returns,
// in order. Native callbacks like __wrap and document are stubbed so
// definition-time access does not throw; the real Go-bound versions
// overwrite the stubs in NewContext after the snapshot is loaded.
//
// Run from repo root: `go run ./cmd/artemis-snapshot/`.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	v8 "rogchap.com/v8go"

	"github.com/Christopher-Schulze/Artemis/js"
)

const snapshotPath = "js/snapshot.bin"

// stubsBootstrap pre-installs no-op or sentinel values for every native
// global and DOM accessor that bootstrap top-level code touches before
// the real Go-bound implementation is bound at NewContext time.
//
// Each stub is overwritten in NewContext, so the values here only matter
// during snapshot creation: they must not throw and must let bootstrap
// IIFEs complete without side effects.
const stubsBootstrap = `
(() => {
  // window === globalThis is set by installWindow but window-events
  // bootstrap assigns onto window before that runs, so we mirror it now.
  globalThis.window = globalThis;

  // Stub document: a plain object so bootstraps can read .documentElement
  // and similar without throwing. Replaced wholesale in installDocument.
  if (!globalThis.document) {
    globalThis.document = Object.create(null);
    globalThis.document.documentElement = null;
    globalThis.document.body = null;
    globalThis.document.head = null;
  }

  // navigator stub for navigator-extras to extend.
  if (!globalThis.navigator) {
    globalThis.navigator = Object.create(null);
  }

  // performance stub: parity-gaps bootstrap installs mark/measure/etc.
  // onto the existing performance object. Without this stub it never
  // sees one (installPerformance runs after snapshot load) and skips
  // the whole branch.
  if (!globalThis.performance) {
    globalThis.performance = Object.create(null);
    globalThis.performance.now = function() { return 0; };
  }

  // location stub: extras bootstrap reads location at top level when it
  // wires up classes referencing the page URL.
  if (!globalThis.location) {
    globalThis.location = {
      href: "", origin: "", protocol: "", host: "", hostname: "",
      port: "", pathname: "/", search: "", hash: "",
    };
  }

  // localStorage / sessionStorage stubs (real ones are bound in
  // installWindow). Bootstraps that touch them at top-level need a
  // benign object.
  if (!globalThis.localStorage) {
    globalThis.localStorage = Object.create(null);
  }
  if (!globalThis.sessionStorage) {
    globalThis.sessionStorage = Object.create(null);
  }

  // crypto stub. crypto bootstraps assign onto crypto.subtle, so both
  // levels must exist as plain objects.
  if (!globalThis.crypto) {
    globalThis.crypto = Object.create(null);
  }
  if (!globalThis.crypto.subtle) {
    globalThis.crypto.subtle = Object.create(null);
  }

  // Native callback stubs. Bootstraps reference dozens of '__name'
  // identifiers; rather than enumerate them, install a benign no-op on
  // globalThis for every '__'-prefixed name a bootstrap touches at
  // top-level. The real Go-bound function replaces these via
  // Global().Set in NewContext.
  //
  // Strategy: do a first pass where missing '__' globals throw a
  // sentinel; run bootstraps; on each ReferenceError, install a stub
  // and retry. Implemented in Go (multiple SnapshotCreator runs are
  // not possible -- the CreateBlob side-effect is one-shot), so we
  // pre-stub the union of names instead.
  //
  // The list below is scraped from grep -rohE '__[a-z_]+' against the
  // bootstrap sources. Keep alphabetised.
  const NATIVE_NAMES = [
    "__append_child","__attr_get","__attr_keys","__attr_remove","__attr_set",
    "__bb_get","__cancel_animation_frame","__cascade_style","__clone_node",
    "__close_event","__console","__cookie_get","__cookie_set","__create_comment",
    "__create_element","__create_event","__create_event_target","__create_text",
    "__crypto_aes_decrypt","__crypto_aes_encrypt","__crypto_aes_kw_unwrap",
    "__crypto_aes_kw_wrap","__crypto_complete","__crypto_derive_aes",
    "__crypto_derive_bits","__crypto_derive_hmac","__crypto_digest",
    "__crypto_ecdh_derive","__crypto_ecdh_generate","__crypto_ecdsa_generate",
    "__crypto_ecdsa_sign","__crypto_ecdsa_verify","__crypto_export_jwk",
    "__crypto_export_pkcs8","__crypto_export_raw","__crypto_export_spki",
    "__crypto_extra","__crypto_generate_aes","__crypto_generate_hmac",
    "__crypto_get_random","__crypto_hkdf","__crypto_hmac","__crypto_import_jwk",
    "__crypto_import_pkcs8","__crypto_import_raw","__crypto_import_spki",
    "__crypto_pbkdf2","__crypto_pkcs8","__crypto_random","__crypto_random_uuid",
    "__crypto_rsa_decrypt","__crypto_rsa_encrypt","__crypto_rsa_generate",
    "__crypto_rsa_sign","__crypto_rsa_verify","__crypto_subtle","__crypto_uuid",
    "__cssom_get","__cssom_set","__data_get","__data_set","__dispatch_event",
    "__document","__dom_token_list","__el_get_input_value","__el_get_input_props",
    "__el_set_input_value","__fetch","__fetch_abort","__fetch_async",
    "__file_array_buffer","__file_text","__formdata_append","__formdata_get",
    "__formdata_pairs","__form_data","__form_get","__form_set","__form_submit",
    "__get_attr","__get_attr_keys","__get_children","__get_html","__get_inner_text",
    "__get_node","__get_outer_html","__get_parent","__get_sibling","__get_text",
    "__headers_get","__headers_init","__html_decode","__html_encode","__iframe_load",
    "__iframe_post","__iframe_get_doc","__input_get","__input_props","__input_set",
    "__insert_before","__list_on_attrs","__list_props","__location","__mkblob",
    "__mkfile","__mkresp","__mutation_observer","__node_at","__node_clear",
    "__node_count","__node_in_doc","__node_lookup","__node_owner","__observer_disconnect",
    "__observer_register","__observer_take","__open_window","__performance_now",
    "__post_message","__query_selector","__query_selector_all","__readable_pull",
    "__reject_promise","__remove_attr","__remove_child","__replace_child",
    "__request_animation_frame","__request_init","__resolve_promise","__resp_blob",
    "__resp_clone","__resp_init","__resp_text","__schedule_microtask","__set_attr",
    "__set_html","__set_inner_text","__set_text","__storage_clear","__storage_get",
    "__storage_key","__storage_length","__storage_remove","__storage_set",
    "__style_compute","__style_get","__style_set","__submit_form","__tagname",
    "__text_decode","__text_encode","__url","__url_search","__wrap","__ws_close",
    "__ws_drain","__ws_open","__ws_send",
  ];
  const noop = function() { return undefined; };
  for (let i = 0; i < NATIVE_NAMES.length; i++) {
    const k = NATIVE_NAMES[i];
    if (!Object.prototype.hasOwnProperty.call(globalThis, k)) {
      globalThis[k] = noop;
    }
  }
})();
`

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "snapshot:", err)
		os.Exit(1)
	}
}

func run() error {
	start := time.Now()

	// --predictable forces V8 to use a deterministic random seed and a
	// stable allocation strategy so the produced snapshot bytes are
	// reproducible across runs. Without it, hash seeds and pointer
	// values vary between invocations, yielding noisy git diffs every
	// time the snapshot is regenerated.
	v8.SetFlags("--predictable")

	sc := v8.NewSnapshotCreator()
	defer sc.Dispose()

	if err := sc.RunScript(stubsBootstrap, "<artemis-snapshot-stubs>"); err != nil {
		return fmt.Errorf("stubs: %w", err)
	}

	for _, b := range js.BootstrapSources() {
		// BootstrapSources already excludes tainted bootstraps; keep this
		// loop simple. Each source is a self-contained IIFE so order only
		// matters for prototype dependencies (dom-bridge first).
		if err := sc.RunScript(b.Source, b.Name); err != nil {
			return fmt.Errorf("bootstrap %s: %w", b.Name, err)
		}
	}

	blob, err := sc.CreateBlob()
	if err != nil {
		return fmt.Errorf("CreateBlob: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(snapshotPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(snapshotPath, blob, 0o644); err != nil {
		return err
	}
	fmt.Printf("snapshot: %s (%d bytes) in %s\n",
		snapshotPath, len(blob), time.Since(start))
	return nil
}
