package actions

import (
	"encoding/json"
	"errors"
	"fmt"
)

type emptyParams struct{}
type emptyResult struct{}

type dispatchKeyEventParams struct {
	Type string `json:"type"`
	Key  string `json:"key,omitempty"`
	Text string `json:"text,omitempty"`
}

type insertTextParams struct {
	Text string `json:"text"`
}

type dispatchMouseEventParams struct {
	Type       string  `json:"type"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Button     string  `json:"button,omitempty"`
	Buttons    int     `json:"buttons,omitempty"`
	ClickCount int     `json:"clickCount,omitempty"`
	DeltaX     float64 `json:"deltaX,omitempty"`
	DeltaY     float64 `json:"deltaY,omitempty"`
}

type setFileInputFilesParams struct {
	Files         []string `json:"files"`
	BackendNodeID int64    `json:"backendNodeId"`
}

type setDownloadBehaviorParams struct {
	Behavior         string `json:"behavior"`
	BrowserContextID string `json:"browserContextId,omitempty"`
	DownloadPath     string `json:"downloadPath,omitempty"`
	EventsEnabled    bool   `json:"eventsEnabled"`
}

type cancelDownloadParams struct {
	GUID             string `json:"guid"`
	BrowserContextID string `json:"browserContextId,omitempty"`
}

type captureScreenshotParams struct {
	Format                string `json:"format"`
	Quality               *int   `json:"quality,omitempty"`
	FromSurface           bool   `json:"fromSurface"`
	CaptureBeyondViewport bool   `json:"captureBeyondViewport"`
}

type printToPDFParams struct {
	PrintBackground bool   `json:"printBackground"`
	TransferMode    string `json:"transferMode"`
}

type handleJavaScriptDialogParams struct {
	Accept     bool   `json:"accept"`
	PromptText string `json:"promptText"`
}

type setDeviceMetricsOverrideParams struct {
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	DeviceScaleFactor float64 `json:"deviceScaleFactor"`
	Mobile            bool    `json:"mobile"`
}

type createIsolatedWorldParams struct {
	FrameID              string `json:"frameId"`
	WorldName            string `json:"worldName,omitempty"`
	GrantUniversalAccess bool   `json:"grantUniveralAccess"`
}

type runtimeEvaluateParams struct {
	Expression    string `json:"expression"`
	ContextID     int64  `json:"contextId,omitempty"`
	ReturnByValue bool   `json:"returnByValue"`
	AwaitPromise  bool   `json:"awaitPromise,omitempty"`
}

type resolveNodeParams struct {
	BackendNodeID int64 `json:"backendNodeId"`
}

type runtimeCallArgument struct {
	Value json.RawMessage `json:"value"`
}

type callFunctionOnParams struct {
	FunctionDeclaration string                `json:"functionDeclaration"`
	ObjectID            string                `json:"objectId"`
	Arguments           []runtimeCallArgument `json:"arguments,omitempty"`
	ReturnByValue       bool                  `json:"returnByValue"`
	AwaitPromise        bool                  `json:"awaitPromise"`
}

type getFrameOwnerParams struct {
	FrameID string `json:"frameId"`
}

type dataResult struct {
	Data string `json:"data"`
}

type createIsolatedWorldResult struct {
	ExecutionContextID int64 `json:"executionContextId"`
}

type runtimeRemoteObject struct {
	Type                string          `json:"type"`
	Value               json.RawMessage `json:"value,omitempty"`
	UnserializableValue string          `json:"unserializableValue,omitempty"`
	Description         string          `json:"description,omitempty"`
	ObjectID            string          `json:"objectId,omitempty"`
}

type runtimeExceptionDetails struct {
	Text string `json:"text"`
}

type runtimeCallResult struct {
	Result           runtimeRemoteObject      `json:"result"`
	ExceptionDetails *runtimeExceptionDetails `json:"exceptionDetails,omitempty"`
}

type resolveNodeResult struct {
	Object runtimeRemoteObject `json:"object"`
}

type getFrameOwnerResult struct {
	BackendNodeID int64 `json:"backendNodeId"`
}

type frameTreeResult struct {
	FrameTree frameBranch `json:"frameTree"`
}

type downloadWillBeginEvent struct {
	GUID              string `json:"guid"`
	SuggestedFilename string `json:"suggestedFilename"`
}

type downloadProgressEvent struct {
	GUID          string  `json:"guid"`
	State         string  `json:"state"`
	ReceivedBytes float64 `json:"receivedBytes"`
}

type viewportDimensions struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type elementRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"w,omitempty"`
	Height float64 `json:"h,omitempty"`
}

type targetMetadata struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type scrollPosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func encodeCallArguments(values []any) ([]runtimeCallArgument, error) {
	arguments := make([]runtimeCallArgument, len(values))
	for i, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode runtime argument %d: %w", i, err)
		}
		arguments[i] = runtimeCallArgument{Value: raw}
	}
	return arguments, nil
}

func (r runtimeCallResult) value() (any, error) {
	if err := r.exceptionError(); err != nil {
		return nil, err
	}
	if len(r.Result.Value) == 0 {
		if r.Result.Type == "undefined" {
			return nil, nil
		}
		if r.Result.UnserializableValue != "" {
			return r.Result.UnserializableValue, nil
		}
		return nil, errors.New("runtime result has no by-value payload")
	}
	var value any
	if err := json.Unmarshal(r.Result.Value, &value); err != nil {
		return nil, fmt.Errorf("decode runtime value: %w", err)
	}
	return value, nil
}

func (r runtimeCallResult) valueInto(destination any) error {
	if err := r.exceptionError(); err != nil {
		return err
	}
	if len(r.Result.Value) == 0 {
		return errors.New("runtime result has no by-value payload")
	}
	if err := json.Unmarshal(r.Result.Value, destination); err != nil {
		return fmt.Errorf("decode typed runtime value: %w", err)
	}
	return nil
}

func (r runtimeCallResult) exceptionError() error {
	if r.ExceptionDetails == nil {
		return nil
	}
	if r.ExceptionDetails.Text == "" {
		return errors.New("runtime evaluation failed")
	}
	return errors.New(r.ExceptionDetails.Text)
}
