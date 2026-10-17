package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/Christopher-Schulze/Artemis/network"
)

// robotsGoverned reports whether robots.txt admission applies: always with
// Config.ObeyRobots, and for crawl fetches unless the operator opted out.
func (e *Engine) robotsGoverned(opts FetchOpts) bool {
	return e.cfg.ObeyRobots || opts.Crawl && !e.cfg.IgnoreRobotsForCrawls
}

// checkRobots admits target for the effective crawler agent and returns it.
func (e *Engine) checkRobots(ctx context.Context, target string, headers http.Header) (string, error) {
	parsed, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("engine: robots target: %w", err)
	}
	validated, err := network.ValidatedHeaders(headers)
	if err != nil {
		return "", err
	}
	userAgent := e.cfg.UserAgent
	if _, supplied := validated["User-Agent"]; supplied {
		userAgent = validated.Get("User-Agent")
	}
	if err := e.client.CheckRobots(ctx, parsed, userAgent); err != nil {
		return "", fmt.Errorf("engine: robots admission: %w", err)
	}
	return userAgent, nil
}
