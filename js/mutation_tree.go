package js

import (
	"errors"

	"github.com/Christopher-Schulze/Artemis/webapi"
)

type mutationEvent struct {
	record mutationRecord
	target *webapi.Node
}

func (c *Context) removalEvent(parent, child *webapi.Node) mutationEvent {
	return mutationEvent{target: parent, record: mutationRecord{Type: MutationChildList, TargetID: c.nodes.Handle(parent),
		RemovedIDs: []uint32{c.nodes.Handle(child)}, PreviousSiblingID: c.nodes.Handle(child.PrevSibling()), NextSiblingID: c.nodes.Handle(child.NextSibling())}}
}

func (c *Context) insertObservedChild(parent, child, reference *webapi.Node) error {
	if reference != nil && reference.Raw().Parent != parent.Raw() {
		return errors.New("ref is not a child of parent")
	}
	var events []mutationEvent
	if source := child.Parent(); source != nil {
		events = append(events, c.removalEvent(source, child))
	}
	if reference != nil && reference.Raw() == child.Raw() {
		reference = child.NextSibling()
	}
	if err := webapi.InsertBefore(parent, child, reference); err != nil {
		return err
	}
	events = append(events, mutationEvent{target: parent, record: mutationRecord{Type: MutationChildList, TargetID: c.nodes.Handle(parent),
		AddedIDs: []uint32{c.nodes.Handle(child)}, PreviousSiblingID: c.nodes.Handle(child.PrevSibling()), NextSiblingID: c.nodes.Handle(child.NextSibling())}})
	return c.observers.recordMutations(c, events)
}

func (c *Context) removeObservedChild(parent, child *webapi.Node) error {
	event := c.removalEvent(parent, child)
	if err := webapi.RemoveChild(parent, child); err != nil {
		return err
	}
	return c.observers.recordMutations(c, []mutationEvent{event})
}
