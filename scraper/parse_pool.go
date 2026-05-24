package scraper

// ParseWorkerPool bounds concurrent HTML parse workers.
type ParseWorkerPool struct {
	workers int
}

func NewParseWorkerPool(workers int) *ParseWorkerPool {
	if workers <= 0 {
		workers = 2
	}
	return &ParseWorkerPool{workers: workers}
}

func (p *ParseWorkerPool) Workers() int { return p.workers }

// SnapshotBuilderPool reuses snapshot builder slots.
type SnapshotBuilderPool struct {
	cap int
}

func NewSnapshotBuilderPool(cap int) *SnapshotBuilderPool {
	if cap <= 0 {
		cap = 4
	}
	return &SnapshotBuilderPool{cap: cap}
}

func (p *SnapshotBuilderPool) Cap() int { return p.cap }
