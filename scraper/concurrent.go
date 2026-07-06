package scraper

import (
	"context"
	"fmt"
	"sync"
)

// ScrapeMode selects static vs rendered concurrent scrape strategy.
type ScrapeMode string

const (
	ModeStaticBulk ScrapeMode = "static_bulk"
	ModeRendered   ScrapeMode = "rendered"
)

// ConcurrentJob is one unit of work in a bulk scrape.
type ConcurrentJob struct {
	URL  string
	Mode ScrapeMode
}

// ConcurrentResult is the per-URL outcome.
type ConcurrentResult struct {
	URL   string
	Body  string
	Error string
}

// ConcurrentScraper runs jobs with bounded worker parallelism.
type ConcurrentScraper struct {
	Workers int
	Scrape  func(context.Context, ConcurrentJob) (string, error)
}

func (c *ConcurrentScraper) Run(ctx context.Context, jobs []ConcurrentJob) ([]ConcurrentResult, error) {
	if c == nil || c.Scrape == nil {
		return nil, fmt.Errorf("concurrent scraper: nil")
	}
	workers := c.Workers
	if workers <= 0 {
		workers = 4
	}
	type indexedJob struct {
		idx int
		job ConcurrentJob
	}
	jobsCh := make(chan indexedJob)
	results := make([]ConcurrentResult, len(jobs))
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range jobsCh {
				if err := ctx.Err(); err != nil {
					errOnce.Do(func() { firstErr = err })
					return
				}
				body, err := c.Scrape(ctx, item.job)
				if err != nil {
					results[item.idx] = ConcurrentResult{URL: item.job.URL, Error: err.Error()}
				} else {
					results[item.idx] = ConcurrentResult{URL: item.job.URL, Body: body}
				}
			}
		}()
	}
	for i, job := range jobs {
		jobsCh <- indexedJob{idx: i, job: job}
	}
	close(jobsCh)
	wg.Wait()
	if firstErr != nil {
		return results, firstErr
	}
	return results, nil
}
