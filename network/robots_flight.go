package network

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrRobotsOwnerClosed = errors.New("robots.txt: owner closed")

type robotsOwner struct{ done chan struct{} }

type robotsFlight struct {
	key, sessionID string
	owner          *robotsOwner
	done           chan struct{}
	cancel         context.CancelFunc
	policy         *RobotsPolicy
	err            error
}

func robotsCacheIdentity(sessionID, origin string) string {
	return fmt.Sprintf("%d:%s%s", len(sessionID), sessionID, origin)
}

func (rc *robotsCache) begin(sessionID, key string) (*RobotsPolicy, *robotsFlight, bool, error) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	if rc.closed {
		return nil, nil, false, ErrRobotsOwnerClosed
	}
	owner := rc.owners[sessionID]
	if owner == nil {
		if len(rc.owners) >= MaxCookieSessions+1 {
			return nil, nil, false, errors.New("robots: session owner limit exceeded")
		}
		owner = &robotsOwner{done: make(chan struct{})}
		rc.owners[sessionID] = owner
	}
	if entry, exists := rc.by[key]; exists {
		if time.Since(entry.fetchedAt) < robotsCacheTTL {
			return cloneRobotsPolicy(entry.policy), nil, false, nil
		}
		rc.removeLocked(key)
	}
	if flight := rc.flights[key]; flight != nil {
		return nil, flight, false, nil
	}
	if len(rc.flights) >= 128 {
		return nil, nil, false, errors.New("robots: in-flight origin limit exceeded")
	}
	flight := &robotsFlight{key: key, sessionID: sessionID, owner: owner, done: make(chan struct{})}
	rc.flights[key] = flight
	return nil, flight, true, nil
}

func (rc *robotsCache) attachCancel(flight *robotsFlight, cancel context.CancelFunc) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	flight.cancel = cancel
	if rc.closed || rc.owners[flight.sessionID] != flight.owner {
		cancel()
	}
}

func (rc *robotsCache) complete(flight *robotsFlight, policy *RobotsPolicy, err error) (*RobotsPolicy, error) {
	rc.mu.Lock()
	if rc.closed || rc.owners[flight.sessionID] != flight.owner {
		policy = nil
		err = errors.Join(err, ErrRobotsOwnerClosed)
	}
	if rc.flights[flight.key] == flight {
		delete(rc.flights, flight.key)
	}
	if err == nil {
		rc.putLocked(flight.key, policy)
	}
	flight.policy, flight.err = cloneRobotsPolicy(policy), err
	close(flight.done)
	rc.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return cloneRobotsPolicy(policy), nil
}

func (flight *robotsFlight) wait(ctx context.Context) (*RobotsPolicy, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-flight.owner.done:
		return nil, ErrRobotsOwnerClosed
	case <-flight.done:
		select {
		case <-flight.owner.done:
			return nil, ErrRobotsOwnerClosed
		default:
		}
		if flight.err != nil {
			return nil, flight.err
		}
		return cloneRobotsPolicy(flight.policy), nil
	}
}
