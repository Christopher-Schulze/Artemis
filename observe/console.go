package observe

// console.go (spec L4024: observe/console.go - console log capture
// ring buffer 1000).
//
// This file is the spec-mandated facade for console log capture.
// The implementation lives in console_buffer.go; this file re-exports
// the key types and functions under the spec-mandated file name.
//
// Page observation: console log capture (ring buffer 1000).

// ConsoleLogEntry is the spec-mandated name for ConsoleEntry
// (spec L4024: console.go - console log capture ring buffer 1000).
type ConsoleLogEntry = ConsoleEntry

// ConsoleLogLevel is the spec-mandated name for ConsoleLevel
// (spec L4024: console.go).
type ConsoleLogLevel = ConsoleLevel

// ConsoleCapture is the spec-mandated name for ConsoleRingBuffer
// (spec L4024: console.go - console log capture ring buffer 1000).
type ConsoleCapture = ConsoleRingBuffer

// DefaultConsoleCaptureCapacity is the spec-mandated default capacity
// (spec L4024: ring buffer 1000).
const DefaultConsoleCaptureCapacity = DefaultConsoleBufferSize

// NewConsoleCapture creates a new ConsoleCapture with the default
// capacity of 1000 (spec L4024: console log capture ring buffer 1000).
func NewConsoleCapture() *ConsoleCapture {
	return NewConsoleRingBuffer(DefaultConsoleBufferSize)
}

// NewConsoleCaptureWithCapacity creates a new ConsoleCapture with a
// custom capacity (spec L4024: console.go).
func NewConsoleCaptureWithCapacity(capacity int) *ConsoleCapture {
	return NewConsoleRingBuffer(capacity)
}

// NormalizeConsoleLevel normalizes a CDP console type to a ConsoleLevel
// (spec L4024: console.go - console log capture).
func NormalizeConsoleLevel(cdpType string) ConsoleLogLevel {
	return NormalizeLevel(cdpType)
}
