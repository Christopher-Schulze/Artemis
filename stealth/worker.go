package stealth

// worker.go (stealth/worker.go - web worker stealth parity).
//
// This file is the spec-mandated facade for web worker stealth parity.
// The implementation lives in worker_stealth.go; this file re-exports
// the key types and functions under the spec-mandated file name.
//
// Anti-detection: web worker stealth parity ensures that stealth
// patches applied to the main page are also applied to web workers
// (service workers, dedicated workers, shared workers).

// WorkerTarget is the spec-mandated name for WorkerTargetInfo
// (worker.go - web worker stealth parity).
type WorkerTarget = WorkerTargetInfo

// NewWorkerTracker is the spec-mandated name for NewWorkerStealthTracker
// (worker.go - web worker stealth parity).
func NewWorkerTracker() *WorkerStealthTracker {
	return NewWorkerStealthTracker()
}
