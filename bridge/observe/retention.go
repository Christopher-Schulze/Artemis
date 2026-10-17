package observe

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

func retentionConfig(config, defaults Config) (Config, error) {
	if config.MaxReferences < 0 || config.MaxReferenceBytes < 0 || config.RetainedEpochs < 0 {
		return Config{}, fmt.Errorf("observation: reference retention budgets cannot be negative")
	}
	if config.MaxReferences == 0 {
		config.MaxReferences = defaults.MaxReferences
	}
	if config.MaxReferenceBytes == 0 {
		config.MaxReferenceBytes = defaults.MaxReferenceBytes
	}
	if config.RetainedEpochs == 0 {
		config.RetainedEpochs = defaults.RetainedEpochs
	}
	return config, nil
}

func (c *Collector) rememberNode(node *Node) error {
	key := identity{epoch: c.epoch, frame: node.FrameID, backend: node.BackendNodeID}
	entry := c.refs[c.identities[key]]
	if entry != nil {
		node.Ref = entry.ref
		if !changed(entry.node, *node) {
			c.touchReference(entry)
			return nil
		}
	} else {
		if c.nextRef == math.MaxUint64 {
			return fmt.Errorf("observation: stable reference identity exhausted")
		}
		c.nextRef++
		node.Ref = "e" + strconv.FormatUint(c.nextRef, 10)
	}
	raw, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("observation: encode reference payload: %w", err)
	}
	if len(raw) > c.config.MaxReferenceBytes {
		if entry != nil {
			c.forgetReference(entry)
		}
		node.Ref = ""
		return nil
	}
	c.retainNode(node, key, entry, len(raw))
	return nil
}

func (c *Collector) retainNode(node *Node, key identity, entry *remembered, bytes int) {
	if entry == nil {
		entry = &remembered{ref: node.Ref, epoch: c.epoch}
		c.refs[node.Ref], c.identities[key] = entry, node.Ref
	}
	c.referenceBytes += bytes - entry.bytes
	entry.node, entry.bytes = cloneNode(*node), bytes
	c.touchReference(entry)
	c.pruneReferences()
}

func (c *Collector) touchReference(entry *remembered) {
	if c.newest == entry {
		return
	}
	if entry.older != nil || entry.newer != nil || c.oldest == entry {
		c.unlinkReference(entry)
	}
	entry.older = c.newest
	if c.newest == nil {
		c.oldest = entry
	} else {
		c.newest.newer = entry
	}
	c.newest = entry
}

func (c *Collector) unlinkReference(entry *remembered) {
	if entry.older == nil {
		c.oldest = entry.newer
	} else {
		entry.older.newer = entry.newer
	}
	if entry.newer == nil {
		c.newest = entry.older
	} else {
		entry.newer.older = entry.older
	}
	entry.older, entry.newer = nil, nil
}

func (c *Collector) forgetReference(entry *remembered) {
	c.unlinkReference(entry)
	delete(c.refs, entry.ref)
	delete(c.identities, identity{epoch: entry.epoch, frame: entry.node.FrameID, backend: entry.node.BackendNodeID})
	c.referenceBytes -= entry.bytes
}

func (c *Collector) pruneReferences() {
	for c.oldest != nil && (len(c.refs) > c.config.MaxReferences || c.referenceBytes > c.config.MaxReferenceBytes || c.epoch-c.oldest.epoch >= uint64(c.config.RetainedEpochs)) {
		c.forgetReference(c.oldest)
	}
}
