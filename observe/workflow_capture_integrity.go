package observe

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
)

func cloneActionCapture(action ActionCapture) ActionCapture {
	action.Selectors = append([]CapturedSelector(nil), action.Selectors...)
	action.AXBefore = cloneCapturedAX(action.AXBefore)
	action.AXAfter = cloneCapturedAX(action.AXAfter)
	if action.Screenshot != nil {
		copied := *action.Screenshot
		action.Screenshot = &copied
	}
	return action
}

func cloneCapturedAX(snapshot *CapturedAXSnapshot) *CapturedAXSnapshot {
	if snapshot == nil {
		return nil
	}
	copied := *snapshot
	copied.Nodes = append([]AXNode(nil), snapshot.Nodes...)
	for i := range copied.Nodes {
		copied.Nodes[i].ChildIDs = append([]string(nil), copied.Nodes[i].ChildIDs...)
	}
	return &copied
}

func cloneWorkflowRecording(recording *WorkflowRecording) *WorkflowRecording {
	copied := *recording
	copied.Actions = make([]ActionCapture, len(recording.Actions))
	for i, action := range recording.Actions {
		copied.Actions[i] = cloneActionCapture(action)
	}
	return &copied
}

func validateCaptureShape(action ActionCapture) error {
	if len(action.Selectors) > 16 || action.ToolCall.Tool == "" || len(action.ToolCall.Tool) > 128 || len(action.ToolCall.ArgsJSON) > 32768 || len(action.ToolCall.ResultJSON) > 32768 {
		return fmt.Errorf("workflow capture: invalid tool/selector bounds")
	}
	if action.ToolCall.ArgsJSON != "" && !json.Valid([]byte(action.ToolCall.ArgsJSON)) || action.ToolCall.ResultJSON != "" && !json.Valid([]byte(action.ToolCall.ResultJSON)) {
		return fmt.Errorf("workflow capture: tool evidence requires valid JSON")
	}
	for _, selector := range action.Selectors {
		knownKind := selector.Kind == SelectorCSS || selector.Kind == SelectorAX || selector.Kind == SelectorXPath || selector.Kind == SelectorUCS
		if !knownKind || selector.Value == "" || len(selector.Value) > 1024 || math.IsNaN(selector.Confidence) || math.IsInf(selector.Confidence, 0) || selector.Confidence < 0 || selector.Confidence > 1 {
			return fmt.Errorf("workflow capture: invalid selector evidence")
		}
	}
	for _, snapshot := range []*CapturedAXSnapshot{action.AXBefore, action.AXAfter} {
		if snapshot != nil && (len(snapshot.Nodes) > 2000 || snapshot.NodeCount < len(snapshot.Nodes)) {
			return fmt.Errorf("workflow capture: invalid AX evidence bounds")
		}
	}
	return nil
}

func validateCapturePayload(action ActionCapture) (int, error) {
	if err := validateCaptureShape(action); err != nil {
		return 0, err
	}
	extra := 0
	if action.Screenshot != nil {
		var err error
		extra, err = validateCapturedPixels(action.Screenshot)
		if err != nil {
			return 0, err
		}
	}
	raw, err := json.Marshal(action)
	if err != nil {
		return 0, fmt.Errorf("workflow capture: encode evidence: %w", err)
	}
	return len(raw) + 1 + extra, nil
}

func validateCapturedPixels(screenshot *CapturedScreenshot) (int, error) {
	const maxPixelsBytes = 4 * 1024 * 1024
	var raw []byte
	extra := 0
	if screenshot.DataB64 != "" && screenshot.Path == "" {
		if len(screenshot.DataB64) > base64.StdEncoding.EncodedLen(maxPixelsBytes) {
			return 0, fmt.Errorf("workflow capture: screenshot byte budget exceeded")
		}
		var err error
		raw, err = base64.StdEncoding.Strict().DecodeString(screenshot.DataB64)
		if err != nil {
			return 0, fmt.Errorf("workflow capture: invalid screenshot encoding")
		}
	} else if screenshot.Path != "" && screenshot.DataB64 == "" {
		info, err := os.Lstat(screenshot.Path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxPixelsBytes {
			return 0, fmt.Errorf("workflow capture: screenshot requires a bounded regular file")
		}
		file, err := os.Open(screenshot.Path)
		if err != nil {
			return 0, fmt.Errorf("workflow capture: open screenshot: %w", err)
		}
		opened, err := file.Stat()
		if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Size() > maxPixelsBytes {
			return 0, errors.Join(fmt.Errorf("workflow capture: screenshot changed before reading"), err, file.Close())
		}
		raw, err = io.ReadAll(io.LimitReader(file, maxPixelsBytes+1))
		err = errors.Join(err, file.Close())
		if err != nil {
			return 0, fmt.Errorf("workflow capture: read screenshot: %w", err)
		}
		if len(raw) > maxPixelsBytes {
			return 0, fmt.Errorf("workflow capture: screenshot grew beyond byte budget")
		}
		extra = len(raw)
	} else {
		return 0, fmt.Errorf("workflow capture: screenshot requires exactly one actual pixel source")
	}
	var config image.Config
	var err error
	pngPixels := bytes.HasPrefix(raw, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	if pngPixels {
		config, err = png.DecodeConfig(bytes.NewReader(raw))
	} else {
		config, err = jpeg.DecodeConfig(bytes.NewReader(raw))
	}
	if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 8_000_000 {
		return 0, fmt.Errorf("workflow capture: invalid or oversized screenshot geometry")
	}
	if pngPixels {
		_, err = png.Decode(bytes.NewReader(raw))
	} else {
		_, err = jpeg.Decode(bytes.NewReader(raw))
	}
	if err != nil {
		return 0, fmt.Errorf("workflow capture: screenshot contains no complete pixels")
	}
	return extra, nil
}

func persistCapturedPixels(screenshot *CapturedScreenshot, dir, recordingID string, index int) error {
	raw, err := base64.StdEncoding.Strict().DecodeString(screenshot.DataB64)
	if err != nil {
		return fmt.Errorf("workflow capture: decode admitted pixels: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("workflow capture: screenshot directory: %w", err)
	}
	extension := "jpg"
	if bytes.HasPrefix(raw, []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		extension = "png"
	}
	hash := sha256.Sum256(raw)
	path := filepath.Join(dir, fmt.Sprintf("%s-%d-%x.%s", recordingID, index, hash[:8], extension))
	if err := writeNewCaptureFile(path, raw); err != nil {
		return fmt.Errorf("workflow capture: persist pixels: %w", err)
	}
	screenshot.Path, screenshot.DataB64 = path, ""
	return nil
}

func writeNewCaptureFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(data)
	if written != len(data) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	closeErr := file.Close()
	return errors.Join(writeErr, closeErr)
}
