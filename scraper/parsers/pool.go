// Package parsers implements the bounded parse phase described by the
// scraper contract. Workers remain pinned to an OS thread for the hot parse
// loop, while lifecycle state is owned by the pool and cancellation is
// propagated to every accepted job.
package parsers

import (
	"bytes"
	"context"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Christopher-Schulze/Artemis/webapi"
	"golang.org/x/net/html"
)

// ParseInput is the immutable input passed to a parser callback.
type ParseInput struct {
	ID   string
	URL  string
	HTML string
}

// ParsedArtifact is the typed output of the parse phase. HTML is retained as
// bytes for downstream evidence, while Document/Title/Text provide the
// canonical parsed representation.
type ParsedArtifact struct {
	URL      string
	HTML     []byte
	Document *webapi.Document
	Title    string
	Text     string
}

// ParseFunc parses one input into a typed artifact.
type ParseFunc func(context.Context, ParseInput) (ParsedArtifact, error)

// ParseJob is a unit of parsing work submitted to the worker pool.
type ParseJob struct {
	ID    string
	URL   string
	HTML  string
	Parse ParseFunc
	Index int
}

// ParseResult is the outcome of a parse job.
type ParseResult struct {
	JobID    string
	Artifact ParsedArtifact
	Error    error
	Worker   int
	Index    int
}

// WorkerPool is a bounded pool of parse workers with lifecycle-safe start,
// submit and stop operations. Jobs are never sent to a closed channel, which
// removes the submit/stop panic window.
type WorkerPool struct {
	workers int
	jobs    chan ParseJob
	results chan ParseResult
	wg      sync.WaitGroup

	startOnce sync.Once
	stopOnce  sync.Once
	sendMu    sync.Mutex
	started   atomic.Bool
	stopped   atomic.Bool
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewWorkerPool creates a parse worker pool with the given number of workers.
// If workers <= 0, runtime.NumCPU() is used.
func NewWorkerPool(workers int) *WorkerPool {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers < 1 {
		workers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		workers: workers,
		jobs:    make(chan ParseJob, workers*2),
		results: make(chan ParseResult, workers*2),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start launches workers exactly once. Starting after Stop is a no-op.
func (p *WorkerPool) Start() {
	if p == nil || p.stopped.Load() {
		return
	}
	p.startOnce.Do(func() {
		if p.stopped.Load() {
			return
		}
		p.started.Store(true)
		for workerID := 0; workerID < p.workers; workerID++ {
			p.wg.Add(1)
			go p.worker(workerID)
		}
	})
}

func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	for {
		job, ok := p.nextJob()
		if !ok {
			return
		}
		result := ParseResult{JobID: job.ID, Worker: id, Index: job.Index}
		parser := job.Parse
		if parser == nil {
			parser = ParseHTML
		}
		result.Artifact, result.Error = parser(p.ctx, ParseInput{ID: job.ID, URL: job.URL, HTML: job.HTML})
		select {
		case p.results <- result:
		case <-p.ctx.Done():
			// Stop cancels the pool and is allowed to release a result consumer
			// that has already gone away. Accepted work is still parsed and the
			// callback receives the cancellation cause.
			return
		}
	}
}

func (p *WorkerPool) nextJob() (ParseJob, bool) {
	select {
	case job := <-p.jobs:
		return job, true
	default:
	}
	if p.ctx.Err() != nil {
		return ParseJob{}, false
	}
	select {
	case job := <-p.jobs:
		return job, true
	case <-p.ctx.Done():
		// Give already-buffered accepted work one last chance to produce a
		// typed cancellation result before the worker exits.
		select {
		case job := <-p.jobs:
			return job, true
		default:
			return ParseJob{}, false
		}
	}
}

// Submit enqueues a parse job. It returns false when the pool has been
// stopped or cancellation wins before the job is accepted.
func (p *WorkerPool) Submit(job ParseJob) bool {
	if p == nil || p.stopped.Load() {
		return false
	}
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	if p.stopped.Load() {
		return false
	}
	p.Start()
	select {
	case <-p.ctx.Done():
		return false
	case p.jobs <- job:
		return true
	}
}

// Results returns the results channel for consuming parse outputs.
func (p *WorkerPool) Results() <-chan ParseResult {
	if p == nil {
		return nil
	}
	return p.results
}

// Workers returns the configured number of workers.
func (p *WorkerPool) Workers() int {
	if p == nil {
		return 0
	}
	return p.workers
}

// Stop cancels the pool exactly once and waits for all started workers. The
// jobs channel remains open so concurrent Submit calls can only observe the
// cancellation state, never panic by sending to a closed channel.
func (p *WorkerPool) Stop() {
	if p == nil {
		return
	}
	p.stopOnce.Do(func() {
		p.stopped.Store(true)
		p.cancel()
		// This lock/unlock is an intentional barrier that joins any in-flight
		// Submit call before Stop proceeds to wait for workers.
		p.sendMu.Lock()
		//lint:ignore SA2001 synchronization barrier; no work belongs inside this section
		p.sendMu.Unlock()
		if p.started.Load() {
			p.wg.Wait()
		}
		close(p.results)
	})
}

// ProcessAll submits all jobs and collects one typed result for every job that
// was accepted. Rejected jobs receive an explicit error result, so callers do
// not mistake cancellation for successful completion.
func (p *WorkerPool) ProcessAll(ctx context.Context, jobs []ParseJob) []ParseResult {
	if p == nil {
		return []ParseResult{{Error: fmt.Errorf("parser pool is nil")}}
	}
	p.Start()
	defer p.Stop()

	results := make([]ParseResult, 0, len(jobs))
	accepted := 0
	for index, job := range jobs {
		job.Index = index
		if p.Submit(job) {
			accepted++
			continue
		}
		results = append(results, ParseResult{JobID: job.ID, Index: index, Error: fmt.Errorf("parse job %q was not accepted", job.ID)})
	}
	for received := 0; received < accepted; received++ {
		select {
		case result, ok := <-p.Results():
			if !ok {
				return results
			}
			results = append(results, result)
		case <-ctx.Done():
			results = append(results, ParseResult{Index: -1, Error: ctx.Err()})
			return results
		}
	}
	sort.SliceStable(results, func(left, right int) bool { return results[left].Index < results[right].Index })
	return results
}

// ParseHTML is the default real parser used when a job does not provide a
// specialized callback.
func ParseHTML(ctx context.Context, input ParseInput) (ParsedArtifact, error) {
	if err := ctx.Err(); err != nil {
		return ParsedArtifact{}, err
	}
	root, err := html.Parse(strings.NewReader(input.HTML))
	if err != nil {
		return ParsedArtifact{}, fmt.Errorf("parse html: %w", err)
	}
	document := webapi.NewDocument(root, input.URL)
	return ParsedArtifact{
		URL:      input.URL,
		HTML:     bytes.Clone([]byte(input.HTML)),
		Document: document,
		Title:    document.Title(),
		Text:     documentText(document.Body()),
	}, nil
}

func documentText(node *webapi.Node) string {
	if node == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*webapi.Node)
	walk = func(current *webapi.Node) {
		if current == nil {
			return
		}
		if current.Type() == webapi.NodeElement {
			tag := current.Tag()
			if tag == "script" || tag == "style" || tag == "noscript" {
				return
			}
		}
		if current.Type() == webapi.NodeText {
			builder.WriteString(current.Data())
			return
		}
		for _, child := range current.Children() {
			walk(child)
		}
	}
	walk(node)
	return strings.TrimSpace(builder.String())
}
