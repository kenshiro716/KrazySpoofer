package response

import (
	"encoding/json"
	"sync"
)

type ResponseItem struct {
	OldID int64 `json:"oldId"`
	NewID int64 `json:"newId"`
}

type Response struct {
	cache       []ResponseItem
	newIDs      []int64
	mutex       sync.RWMutex
	onItemAdded func(i ResponseItem)
}

func New(onItemAdded ...func(i ResponseItem)) *Response {
	var callback func(i ResponseItem)
	if len(onItemAdded) > 0 {
		callback = onItemAdded[0]
	}

	return &Response{
		cache:       make([]ResponseItem, 0),
		onItemAdded: callback,
	}
}

func (r *Response) AddItem(i ResponseItem) {
	r.mutex.Lock()

	r.cache = append(r.cache, i)
	r.newIDs = append(r.newIDs, i.NewID)
	if r.onItemAdded != nil {
		go r.onItemAdded(i)
	}

	r.mutex.Unlock()
}

// TakeNewIDs returns every new id added since the last call, without duplicates.
func (r *Response) TakeNewIDs() []int64 {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	seen := make(map[int64]struct{}, len(r.newIDs))
	ids := make([]int64, 0, len(r.newIDs))
	for _, id := range r.newIDs {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	r.newIDs = nil
	return ids
}

// Items returns a copy of the items waiting to be sent.
func (r *Response) Items() []ResponseItem {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return append([]ResponseItem(nil), r.cache...)
}

func (r *Response) Clear() {
	r.mutex.Lock()
	r.cache = make([]ResponseItem, 0)
	r.mutex.Unlock()
}

func (r *Response) EncodeJSON(e *json.Encoder) error {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return e.Encode(r.cache)
}

func (r *Response) Len() int {
	r.mutex.RLock()
	defer r.mutex.RUnlock()
	return len(r.cache)
}
