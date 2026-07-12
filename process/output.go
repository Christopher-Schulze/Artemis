package process

import "sync"

type cappedOutput struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func newCappedOutput(limit int) *cappedOutput {
	return &cappedOutput{limit: limit}
}

func (o *cappedOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	written := len(p)
	if len(p) >= o.limit {
		o.data = append(o.data[:0], p[len(p)-o.limit:]...)
		return written, nil
	}
	o.data = append(o.data, p...)
	if overflow := len(o.data) - o.limit; overflow > 0 {
		copy(o.data, o.data[overflow:])
		o.data = o.data[:o.limit]
	}
	return written, nil
}

func (o *cappedOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return string(append([]byte(nil), o.data...))
}
