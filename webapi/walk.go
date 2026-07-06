package webapi

import "golang.org/x/net/html"

// WalkAction tells Walk how to continue.
type WalkAction int

const (
	// WalkContinue descends into the children of the current node.
	WalkContinue WalkAction = iota
	// WalkSkip skips the children of the current node.
	WalkSkip
	// WalkStop terminates the walk entirely.
	WalkStop
)

// Walk visits the node tree in pre-order, calling fn at every node.
// fn returns one of the WalkAction values to control descent.
func Walk(n *Node, fn func(*Node) WalkAction) {
	if n == nil || fn == nil {
		return
	}
	walkRaw(n.raw, fn)
}

// WalkDocument visits every node of the document in pre-order.
func WalkDocument(d *Document, fn func(*Node) WalkAction) {
	if d == nil || fn == nil {
		return
	}
	walkRaw(d.root, fn)
}

// WalkValue visits the node tree in pre-order like Walk, but passes each Node BY
// VALUE (Node is a single pointer) instead of allocating a *Node wrapper per visit.
// Use it for callbacks that only READ the node and never retain its address across
// the walk; it eliminates the per-node heap allocation Walk pays for retention
// safety. A caller that needs to keep one node takes its address explicitly, which
// escapes only that node, not every visit.
func WalkValue(n *Node, fn func(Node) WalkAction) {
	if n == nil || fn == nil {
		return
	}
	walkRawValue(n.raw, fn)
}

func walkRawValue(n *html.Node, fn func(Node) WalkAction) bool {
	if n == nil {
		return true
	}
	switch fn(Node{raw: n}) {
	case WalkStop:
		return false
	case WalkSkip:
		return true
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if !walkRawValue(c, fn) {
			return false
		}
	}
	return true
}

func walkRaw(n *html.Node, fn func(*Node) WalkAction) bool {
	if n == nil {
		return true
	}
	// Each visit gets its own *Node wrapper because callers commonly
	// retain c (e.g. GetElementsByTagName appending to a slice). A
	// shared wrapper would alias every returned pointer to the last
	// visited node.
	switch fn(&Node{raw: n}) {
	case WalkStop:
		return false
	case WalkSkip:
		return true
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if !walkRaw(c, fn) {
			return false
		}
	}
	return true
}
