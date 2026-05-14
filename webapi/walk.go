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
