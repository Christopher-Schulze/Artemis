package network

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

type robotsDelayGate struct {
	turn      chan struct{}
	last      time.Time
	borrowers int
}

// CheckRobots admits a configured crawler request and its origin crawl delay.
// The caller owns cancellation; it runs before the actual page request lease.
func (c *HTTPClient) CheckRobots(ctx context.Context, target *url.URL, userAgent string) error {
	policy, err := c.fetchRobots(ctx, target, &userAgent)
	if err != nil {
		return err
	}
	path := target.EscapedPath()
	if path == "" {
		path = "/"
	}
	if target.RawQuery != "" {
		path += "?" + target.RawQuery
	}
	if !policy.Allowed(userAgent, path) {
		return ErrRobotsDisallowed
	}
	delay := policy.CrawlDelayFor(userAgent)
	if delay == 0 {
		return ctx.Err()
	}
	origin, err := robotsOriginURL(target)
	if err != nil {
		return err
	}
	return c.robots.waitDelay(ctx, SessionID(ctx, c.cfg.SessionID), origin, delay)
}

func (rc *robotsCache) borrowDelay(sessionID, origin string) (*robotsDelayGate, *robotsOwner, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	owner := rc.owners[sessionID]
	if rc.closed || owner == nil {
		return nil, nil, ErrRobotsOwnerClosed
	}
	gate := rc.delayGates[origin]
	if gate == nil {
		if len(rc.delayGates) >= robotsCacheMaxHosts {
			for key, candidate := range rc.delayGates {
				if candidate.borrowers == 0 && time.Since(candidate.last) >= robotsCacheTTL {
					delete(rc.delayGates, key)
				}
			}
		}
		if len(rc.delayGates) >= robotsCacheMaxHosts {
			return nil, nil, errors.New("robots: crawl-delay origin limit exceeded")
		}
		gate = &robotsDelayGate{turn: make(chan struct{}, 1)}
		gate.turn <- struct{}{}
		rc.delayGates[origin] = gate
	}
	gate.borrowers++
	return gate, owner, nil
}

func (rc *robotsCache) waitDelay(ctx context.Context, sessionID, origin string, delay time.Duration) error {
	gate, owner, err := rc.borrowDelay(sessionID, origin)
	if err != nil {
		return err
	}
	defer func() { rc.mu.Lock(); gate.borrowers--; rc.mu.Unlock() }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-owner.done:
		return ErrRobotsOwnerClosed
	case <-gate.turn:
	}
	defer func() { gate.turn <- struct{}{} }()
	rc.mu.Lock()
	last := gate.last
	rc.mu.Unlock()
	wait := time.Until(last.Add(delay))
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-owner.done:
			return ErrRobotsOwnerClosed
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.closed || rc.owners[sessionID] != owner {
		return fmt.Errorf("%w: crawl owner retired", ErrRobotsOwnerClosed)
	}
	gate.last = time.Now()
	return nil
}
