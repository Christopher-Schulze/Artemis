package js

import (
	"strconv"
	"strings"
)

const snapshotStubMarker = "__ARTEMIS_NATIVE_STUB_NAMES__"

const snapshotStubsBootstrapTemplate = `
(() => {
  globalThis.window = globalThis;
  if (!globalThis.document) {
    globalThis.document = Object.create(null);
    globalThis.document.documentElement = null;
    globalThis.document.body = null;
    globalThis.document.head = null;
  }
  if (!globalThis.navigator) {
    globalThis.navigator = Object.create(null);
  }
  if (!globalThis.performance) {
    globalThis.performance = Object.create(null);
    globalThis.performance.now = function() { return 0; };
  }
  if (!globalThis.location) {
    globalThis.location = {
      href: "", origin: "", protocol: "", host: "", hostname: "",
      port: "", pathname: "/", search: "", hash: "",
    };
  }
  if (!globalThis.localStorage) {
    globalThis.localStorage = Object.create(null);
  }
  if (!globalThis.sessionStorage) {
    globalThis.sessionStorage = Object.create(null);
  }
  if (!globalThis.crypto) {
    globalThis.crypto = Object.create(null);
  }
  if (!globalThis.crypto.subtle) {
    globalThis.crypto.subtle = Object.create(null);
  }
  const NATIVE_NAMES = [
    __ARTEMIS_NATIVE_STUB_NAMES__
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

// This function returns the exact deterministic prelude passed to V8's
// SnapshotCreator. The callback inventory is generated from the canonical Go
// list rather than maintained in a second hand-written list.
func SnapshotStubBootstrap() string {
	names := SnapshotNativeStubNames()
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, "    "+strconv.Quote(name))
	}
	return strings.Replace(snapshotStubsBootstrapTemplate, snapshotStubMarker, strings.Join(quoted, ",\n"), 1)
}
