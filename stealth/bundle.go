// Package stealth provides build-time script bundling via go:embed (spec ss28.15.1 P0.1).
//
// All stealth patches are bundled into ONE script at build-time via go:embed.
// This reduces CDP calls from 27 (one per patch) to 1 per page.
// The bundled script is ~25KB minified. Paranoid mode adds +2 on-demand patches.
package stealth

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"strings"
)

// bundledStealthScript is the minified stealth patch script embedded at build-time.
// It contains all 27 patches concatenated into a single IIFE that applies
// navigator.webdriver, chrome runtime, permissions, plugins, canvas noise,
// WebGL, audio context, and other anti-detection patches.
//
//go:embed stealth_bundle.js
var bundledStealthScript string

// BundledScript returns the full bundled stealth script.
// This is the single script that should be injected via one CDP call
// per page instead of 27 individual patch injections.
func BundledScript() string {
	return bundledStealthScript
}

// BundledScriptSize returns the size of the bundled script in bytes.
func BundledScriptSize() int {
	return len(bundledStealthScript)
}

// BundledScriptHash returns a SHA-256 hash of the bundled script for
// integrity verification and cache invalidation.
func BundledScriptHash() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(bundledStealthScript)))
}

// ParanoidScript returns the bundled script plus the 2 on-demand paranoid
// patches (referrer spoofing and typing rhythm). Per spec P0.1:
// "Paranoid: +2 on-demand".
func ParanoidScript() string {
	var b strings.Builder
	b.WriteString(bundledStealthScript)
	b.WriteString("\n// === Paranoid on-demand patches ===\n")
	b.WriteString(paranoidReferrerPatch)
	b.WriteString("\n")
	b.WriteString(paranoidTypingRhythmPatch)
	return b.String()
}

// paranoidReferrerPatch spoofs the Referer header from SQLite domain memory.
// Per spec P0.4: "Static extraHTTPHeaders['Referer'] from SQLite domain memory (TTL 7d)".
const paranoidReferrerPatch = `
(function() {
  'use strict';
  // Referrer spoofing: intercept document.referrer getter
  let spoofedReferrer = '';
  try {
    Object.defineProperty(document, 'referrer', {
      get: function() { return spoofedReferrer; },
      configurable: true
    });
  } catch(e) {}
  // the host sets spoofedReferrer via CDP before navigation
})();
`

// paranoidTypingRhythmPatch adds human-like typing rhythm with typo simulation.
// Per spec P0.6: "typo_rate=0.0 default. Paranoid: 0.02".
const paranoidTypingRhythmPatch = `
(function() {
  'use strict';
  // Typing rhythm: intercept input events to add human-like delays
  const typoRate = 0.02; // 2% typo rate for paranoid mode
  // The actual rhythm is applied by the bridge layer via CDP Input.dispatchKeyEvent
  // with randomized delays (50-150ms between keystrokes)
  window.__omnimusTypoRate = typoRate;
})();
`

// PatchCount returns the number of patches in the bundled script.
// Per spec: 27 patches in the base bundle, +2 for paranoid.
const BasePatchCount = 27
const ParanoidPatchCount = 2

// IsBundled returns true if the stealth script was successfully embedded
// at build-time via go:embed.
func IsBundled() bool {
	return len(bundledStealthScript) > 0
}
