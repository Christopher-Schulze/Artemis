package js

import (
	"errors"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

type transientKey struct{ targetID, sourceID uint32 }
type transientOrigin struct {
	owner    *observer
	sourceID uint32
}
type transientIdentity struct {
	origin   transientOrigin
	targetID uint32
}
type transientRegistration struct {
	origin   transientOrigin
	targetID uint32
	options  observerOptions
}

func (c *Context) mutationContextError() error {
	if err := c.async.ctx.Err(); err != nil {
		return err
	}
	if c.evalCtx != nil {
		return c.evalCtx.Err()
	}
	return nil
}

// removalSources retains original registration identity across nested detaches.
// The caller holds r.mu and the isolate execution owner.
func (r *observerRegistry) removalSources(c *Context, parent *webapi.Node) (map[transientOrigin]observerOptions, error) {
	sources := make(map[transientOrigin]observerOptions)
	for node := parent; node != nil; node = node.Parent() {
		if err := c.mutationContextError(); err != nil {
			return nil, err
		}
		id := c.nodes.Handle(node)
		for _, owner := range r.byTarget[id] {
			options := owner.targets[id]
			if options.subtree {
				sources[transientOrigin{owner, id}] = options
			}
		}
		for _, registration := range r.transientTargets[id] {
			if registration.options.subtree {
				sources[registration.origin] = registration.options
			}
		}
	}
	return sources, nil
}

func (r *observerRegistry) planTransients(c *Context, events []mutationEvent) ([]*transientRegistration, error) {
	if len(events) == 1 && len(events[0].record.RemovedIDs) == 0 {
		return nil, nil
	}
	var planned []*transientRegistration
	seen := make(map[transientIdentity]bool)
	bytes := r.optionsBytes
	for _, event := range events {
		if len(event.record.RemovedIDs) == 0 {
			continue
		}
		sources, err := r.removalSources(c, event.target)
		if err != nil {
			return nil, err
		}
		if len(sources) == 0 {
			continue
		}
		for _, targetID := range event.record.RemovedIDs {
			if err := c.mutationContextError(); err != nil {
				return nil, err
			}
			for origin, options := range sources {
				identity := transientIdentity{origin, targetID}
				key := transientKey{targetID, origin.sourceID}
				if seen[identity] || origin.owner.transients[key] != nil {
					continue
				}
				if r.registrations+len(planned) >= maxMutationRegistrationsPerContext {
					return nil, errors.New("mutation observer transient registration limit exceeded")
				}
				cost := options.retainedBytes()
				if cost > maxMutationPendingBytes-bytes {
					return nil, errors.New("mutation observer transient options byte limit exceeded")
				}
				bytes += cost
				seen[identity] = true
				planned = append(planned, &transientRegistration{origin: origin, targetID: targetID, options: options})
			}
		}
	}
	return planned, nil
}

func (r *observerRegistry) commitTransients(planned []*transientRegistration) {
	for _, registration := range planned {
		owner := registration.origin.owner
		if owner.transients == nil {
			owner.transients = make(map[transientKey]*transientRegistration)
		}
		owner.transients[transientKey{registration.targetID, registration.origin.sourceID}] = registration
		r.transientTargets[registration.targetID] = append(r.transientTargets[registration.targetID], registration)
		r.registrations++
		r.optionsBytes += registration.options.retainedBytes()
	}
}

func (r *observerRegistry) transientBytes(owner *observer, sourceID uint32) int {
	bytes := 0
	for key, registration := range owner.transients {
		if sourceID == 0 || key.sourceID == sourceID {
			bytes += registration.options.retainedBytes()
		}
	}
	return bytes
}

// removeTransients requires r.mu. A zero sourceID retires all inherited roots.
func (r *observerRegistry) removeTransients(owner *observer, sourceID uint32) {
	for key, registration := range owner.transients {
		if sourceID != 0 && key.sourceID != sourceID {
			continue
		}
		entries := r.transientTargets[key.targetID]
		kept := entries[:0]
		for _, entry := range entries {
			if entry != registration {
				kept = append(kept, entry)
			}
		}
		clear(entries[len(kept):])
		if len(kept) == 0 {
			delete(r.transientTargets, key.targetID)
		} else {
			r.transientTargets[key.targetID] = kept
		}
		delete(owner.transients, key)
		r.registrations--
		r.optionsBytes -= registration.options.retainedBytes()
	}
	if len(owner.transients) == 0 {
		owner.transients = nil
	}
}
