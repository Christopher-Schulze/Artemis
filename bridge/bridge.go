package bridge

// bridge.go (bridge/bridge.go - CDP Bridge - Chrome
// control, Context Hierarchy).
//
// This file is the spec-mandated facade for the CDP Bridge.
// The implementation lives in provider.go, cdp_context.go,
// browser_ops.go; this file re-exports the key types and functions
// under the spec-mandated file name.
//
// CDP Bridge - Chrome control. Context Hierarchy (AllocCtx ->
// BrowserCtx -> TabCtx), Lifecycle, Tab Registry, Chrome Launch +
// Stealth Injection, Bridge State Machine.

// ContextKind enumerates CDP context hierarchy levels
// (Context Hierarchy AllocCtx -> BrowserCtx -> TabCtx).
type ContextKind string

const (
	// ContextKindAlloc is the allocation context (root).
	ContextKindAlloc ContextKind = "alloc"
	// ContextKindBrowser is the browser context.
	ContextKindBrowser ContextKind = "browser"
	// ContextKindTab is the tab context (leaf).
	ContextKindTab ContextKind = "tab"
)

// BridgeContextUnit is the public alias for CDPContextUnit
// (Context Hierarchy).
type BridgeContextUnit = CDPContextUnit

// BridgeContextTree is the spec-mandated alias for CDPContextTree
// (Context Hierarchy).
type BridgeContextTree = CDPContextTree

// NewBridgeContextTree creates a new CDP context tree
// (Context Hierarchy).
func NewBridgeContextTree() *BridgeContextTree {
	return NewCDPContextTree()
}

// BridgeProvider is the spec-mandated alias for BrowserProvider
// (Chrome control).
type BridgeProvider = BrowserProvider

// BridgeSession is the spec-mandated alias for BrowserSession
// (Chrome control).
type BridgeSession = BrowserSession

// BridgeProviderRegistry is the spec-mandated alias for
// ProviderRegistry (Chrome Launch).
type BridgeProviderRegistry = ProviderRegistry

// NewBridgeProviderRegistry creates a new provider registry
// (Chrome Launch).
func NewBridgeProviderRegistry() *BridgeProviderRegistry {
	return NewProviderRegistry()
}

// IsValidContextKind reports whether a context kind is valid
// (Context Hierarchy AllocCtx -> BrowserCtx -> TabCtx).
func IsValidContextKind(kind ContextKind) bool {
	switch kind {
	case ContextKindAlloc, ContextKindBrowser, ContextKindTab:
		return true
	}
	return false
}

// IsParentContext reports whether a context kind can be a parent
// (Context Hierarchy).
func IsParentContext(kind ContextKind) bool {
	return kind == ContextKindAlloc || kind == ContextKindBrowser
}

// IsLeafContext reports whether a context kind is a leaf (tab)
// (Context Hierarchy).
func IsLeafContext(kind ContextKind) bool {
	return kind == ContextKindTab
}
