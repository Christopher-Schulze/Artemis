package process

import "sync"

type cappedOutput struct {
	mu        sync.Mutex
	limit     int
	data      []byte
	truncated bool
}

func newCappedOutput(limit int) *cappedOutput {
	return &cappedOutput{limit: limit}
}

func (o *cappedOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	written := len(p)
	if len(p) >= o.limit {
		if len(o.data)+len(p) > o.limit {
			o.truncated = true
		}
		o.data = append(o.data[:0], p[len(p)-o.limit:]...)
		return written, nil
	}
	o.data = append(o.data, p...)
	if overflow := len(o.data) - o.limit; overflow > 0 {
		o.truncated = true
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

func (o *cappedOutput) Truncated() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.truncated
}
