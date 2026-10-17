package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	// TraceEntryBytesLimit bounds each uncompressed archive entry.
	TraceEntryBytesLimit = 8 << 20
	// TraceTotalBytesLimit bounds retained payload/debug bytes and ZIP output.
	TraceTotalBytesLimit = 64 << 20
	// TraceArtifactLimit bounds screenshot, snapshot and source captures together.
	TraceArtifactLimit   = 128
	traceDebugBytesLimit = 8 << 20
	traceFixedEntries    = 4
)

func (r *TraceRecorder) reserveArtifact(size int) error {
	if size > TraceEntryBytesLimit || r.artifactCount >= TraceArtifactLimit || int64(size) > TraceTotalBytesLimit-r.retainedBytes {
		return fmt.Errorf("trace artifact exceeds entry, count or aggregate byte limit")
	}
	r.artifactCount++
	r.retainedBytes += int64(size)
	return nil
}

func measureTraceDebug(evidence TraceDebugEvidence) (int64, error) {
	if len(evidence.Console) > traceDebugEntryLimit || len(evidence.PageErrors) > traceDebugEntryLimit || len(evidence.Network) > traceDebugEntryLimit {
		return 0, fmt.Errorf("trace debug entry count exceeds limit")
	}
	bytes := 0
	check := func(values ...string) bool {
		for _, value := range values {
			if len(value) > traceTextLimit || len(value) > traceDebugBytesLimit-bytes {
				return false
			}
			bytes += len(value)
		}
		return true
	}
	if !check(evidence.Target.TargetID, evidence.Target.SessionID, evidence.Target.BrowserContextID) {
		return 0, fmt.Errorf("trace debug target exceeds limit")
	}
	for _, entry := range evidence.Console {
		if !check(entry.Type, entry.Text, entry.URL) {
			return 0, fmt.Errorf("trace console evidence exceeds text/byte limit")
		}
	}
	for _, entry := range evidence.PageErrors {
		if !check(entry.Message, entry.Name, entry.Stack, entry.URL) {
			return 0, fmt.Errorf("trace page-error evidence exceeds text/byte limit")
		}
	}
	for _, entry := range evidence.Network {
		if !check(entry.RequestID, entry.FrameID, entry.LoaderID, entry.Method, entry.URL, entry.ResourceType, entry.FailureText) {
			return 0, fmt.Errorf("trace network evidence exceeds text/byte limit")
		}
	}
	var total int64
	console, consoleErr := json.Marshal(evidence.Console)
	pageErrors, pageErr := json.Marshal(evidence.PageErrors)
	network, networkErr := json.Marshal(evidence.Network)
	if err := errors.Join(consoleErr, pageErr, networkErr); err != nil {
		return 0, fmt.Errorf("encode trace debug evidence: %w", err)
	}
	for _, encoded := range [][]byte{console, pageErrors, network} {
		if len(encoded) > TraceEntryBytesLimit {
			return 0, fmt.Errorf("encoded trace debug entry exceeds byte limit")
		}
		total += int64(len(encoded))
	}
	return total, nil
}

type traceArchiveWriter struct {
	writer    io.Writer
	remaining int
}

func (w *traceArchiveWriter) Write(data []byte) (int, error) {
	if len(data) > w.remaining {
		return 0, fmt.Errorf("trace ZIP exceeds %d bytes", TraceTotalBytesLimit)
	}
	n, err := w.writer.Write(data)
	w.remaining -= n
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return n, err
}
