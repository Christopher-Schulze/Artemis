package cdpops

type emptyResult struct{}

type navigateParams struct {
	URL      string `json:"url"`
	Referrer string `json:"referrer,omitempty"`
}

type reloadParams struct {
	IgnoreCache bool `json:"ignoreCache"`
}

type navigationHistoryParams struct{}

type navigationHistoryEntry struct {
	ID  int64  `json:"id"`
	URL string `json:"url"`
}

type navigationHistoryResult struct {
	CurrentIndex int                      `json:"currentIndex"`
	Entries      []navigationHistoryEntry `json:"entries"`
}

type navigateToHistoryEntryParams struct {
	EntryID int64 `json:"entryId"`
}

type runtimeEvaluateParams struct {
	Expression    string `json:"expression"`
	ReturnByValue bool   `json:"returnByValue"`
}

type readyStateResult struct {
	Result struct {
		Value string `json:"value"`
	} `json:"result"`
}

type dispatchMouseEventParams struct {
	Type       string   `json:"type"`
	X          float64  `json:"x"`
	Y          float64  `json:"y"`
	Button     string   `json:"button,omitempty"`
	ClickCount int      `json:"clickCount,omitempty"`
	DeltaX     *float64 `json:"deltaX,omitempty"`
	DeltaY     *float64 `json:"deltaY,omitempty"`
}

type touchPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type dispatchTouchEventParams struct {
	Type        string       `json:"type"`
	TouchPoints []touchPoint `json:"touchPoints"`
}

type getDocumentParams struct {
	Depth  int  `json:"depth"`
	Pierce bool `json:"pierce"`
}

type getDocumentResult struct {
	Root struct {
		NodeID int64 `json:"nodeId"`
	} `json:"root"`
}

type querySelectorAllParams struct {
	NodeID   int64  `json:"nodeId"`
	Selector string `json:"selector"`
}

type querySelectorAllResult struct {
	NodeIDs []int64 `json:"nodeIds"`
}

type describeNodeParams struct {
	NodeID int64 `json:"nodeId"`
}

type describeNodeResult struct {
	Node struct {
		BackendNodeID int64    `json:"backendNodeId"`
		NodeName      string   `json:"nodeName"`
		Attributes    []string `json:"attributes"`
	} `json:"node"`
}

type getBoxModelParams struct {
	BackendNodeID int64 `json:"backendNodeId"`
}

type getBoxModelResult struct {
	Model struct {
		Content []float64 `json:"content"`
		Padding []float64 `json:"padding"`
		Border  []float64 `json:"border"`
		Margin  []float64 `json:"margin"`
		Width   int       `json:"width"`
		Height  int       `json:"height"`
	} `json:"model"`
}
