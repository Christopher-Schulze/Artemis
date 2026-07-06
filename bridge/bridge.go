package bridge

// bridge.go (spec L4018: bridge/bridge.go - CDP Bridge - Chrome
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
// (spec L4018: Context Hierarchy AllocCtx -> BrowserCtx -> TabCtx).
type ContextKind string

const (
	// ContextKindAlloc is the allocation context (root).
	ContextKindAlloc ContextKind = "alloc"
	// ContextKindBrowser is the browser context.
	ContextKindBrowser ContextKind = "browser"
	// ContextKindTab is the tab context (leaf).
	ContextKindTab ContextKind = "tab"
)

// BridgeContextNode is the spec-mandated alias for CDPContextNode
// (spec L4018: Context Hierarchy).
type BridgeContextNode = CDPContextNode

// BridgeContextTree is the spec-mandated alias for CDPContextTree
// (spec L4018: Context Hierarchy).
type BridgeContextTree = CDPContextTree

// NewBridgeContextTree creates a new CDP context tree
// (spec L4018: Context Hierarchy).
func NewBridgeContextTree() *BridgeContextTree {
	return NewCDPContextTree()
}

// BridgeProvider is the spec-mandated alias for BrowserProvider
// (spec L4018: Chrome control).
type BridgeProvider = BrowserProvider

// BridgeSession is the spec-mandated alias for BrowserSession
// (spec L4018: Chrome control).
type BridgeSession = BrowserSession

// BridgeProviderRegistry is the spec-mandated alias for
// ProviderRegistry (spec L4018: Chrome Launch).
type BridgeProviderRegistry = ProviderRegistry

// NewBridgeProviderRegistry creates a new provider registry
// (spec L4018: Chrome Launch).
func NewBridgeProviderRegistry() *BridgeProviderRegistry {
	return NewProviderRegistry()
}

// IsValidContextKind reports whether a context kind is valid
// (spec L4018: Context Hierarchy AllocCtx -> BrowserCtx -> TabCtx).
func IsValidContextKind(kind ContextKind) bool {
	switch kind {
	case ContextKindAlloc, ContextKindBrowser, ContextKindTab:
		return true
	}
	return false
}

// IsParentContext reports whether a context kind can be a parent
// (spec L4018: Context Hierarchy).
func IsParentContext(kind ContextKind) bool {
	return kind == ContextKindAlloc || kind == ContextKindBrowser
}

// IsLeafContext reports whether a context kind is a leaf (tab)
// (spec L4018: Context Hierarchy).
func IsLeafContext(kind ContextKind) bool {
	return kind == ContextKindTab
}
