package scraper

import (
	"context"
	"fmt"

	"github.com/Christopher-Schulze/Artemis/scraper/parsers"
)

// ParseWorkerPool bounds concurrent HTML parse workers.
type ParseWorkerPool struct {
	workers int
}

// ParsedPage is the serializable summary of the typed parser artifact.
type ParsedPage struct {
	URL   string `json:"url"`
	HTML  string `json:"html"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

func NewParseWorkerPool(workers int) *ParseWorkerPool {
	if workers <= 0 {
		workers = 2
	}
	return &ParseWorkerPool{workers: workers}
}

func (p *ParseWorkerPool) Workers() int { return p.workers }

// Parse executes one real HTML parse through a bounded worker runtime and
// returns its typed artifact.
func (p *ParseWorkerPool) Parse(ctx context.Context, id, pageURL, html string) (parsers.ParsedArtifact, error) {
	if p == nil {
		return parsers.ParsedArtifact{}, fmt.Errorf("parse worker pool is nil")
	}
	results := p.ProcessAll(ctx, []parsers.ParseJob{{ID: id, URL: pageURL, HTML: html}})
	if len(results) != 1 {
		return parsers.ParsedArtifact{}, fmt.Errorf("parse worker pool: expected one result, got %d", len(results))
	}
	if results[0].Error != nil {
		return parsers.ParsedArtifact{}, results[0].Error
	}
	return results[0].Artifact, nil
}

// ProcessAll executes a batch through a fresh lifecycle-safe runtime. A
// ParseWorkerPool is a configuration facade, so each batch owns and closes
// its worker resources.
func (p *ParseWorkerPool) ProcessAll(ctx context.Context, jobs []parsers.ParseJob) []parsers.ParseResult {
	if p == nil {
		return []parsers.ParseResult{{Error: fmt.Errorf("parse worker pool is nil")}}
	}
	runtime := parsers.NewWorkerPool(p.workers)
	return runtime.ProcessAll(ctx, jobs)
}

// ParsePage exposes the canonical parser runtime to product callers without
// leaking the mutable DOM wrapper across the browser boundary.
func ParsePage(ctx context.Context, id, pageURL, html string) (ParsedPage, error) {
	artifact, err := NewParseWorkerPool(1).Parse(ctx, id, pageURL, html)
	if err != nil {
		return ParsedPage{}, err
	}
	return ParsedPage{URL: artifact.URL, HTML: string(artifact.HTML), Title: artifact.Title, Text: artifact.Text}, nil
}

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
