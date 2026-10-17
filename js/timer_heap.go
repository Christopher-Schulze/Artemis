package js

type timerHeap []*timerEntry

func (h timerHeap) Len() int { return len(h) }

func (h timerHeap) Less(i, j int) bool {
	if h[i].due.Equal(h[j].due) {
		return h[i].id < h[j].id
	}
	return h[i].due.Before(h[j].due)
}

func (h timerHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index, h[j].index = i, j
}

func (h *timerHeap) Push(value any) {
	entry := value.(*timerEntry)
	entry.index = len(*h)
	*h = append(*h, entry)
}

func (h *timerHeap) Pop() any {
	last := len(*h) - 1
	entry := (*h)[last]
	(*h)[last] = nil
	*h = (*h)[:last]
	entry.index = -1
	return entry
}
