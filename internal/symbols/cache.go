package symbols

import (
	"container/list"
	"sync"
)

// sizedCache is an LRU cache bounded by the estimated memory of its values
// (parsed source maps and debug information indexes vary from kilobytes to
// hundreds of megabytes, so a count limit would not do).
type sizedCache struct {
	mu    sync.Mutex
	limit int
	used  int
	ll    *list.List
	items map[string]*list.Element
}

type cacheEntry struct {
	key   string
	value any
	size  int
}

func newSizedCache(limit int) *sizedCache {
	return &sizedCache{limit: limit, ll: list.New(), items: map[string]*list.Element{}}
}

func (c *sizedCache) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*cacheEntry).value, true
	}
	return nil, false
}

// add stores a value; values larger than the whole cache are not kept.
func (c *sizedCache) add(key string, value any, size int) {
	if size > c.limit {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		e := el.Value.(*cacheEntry)
		c.used += size - e.size
		e.value, e.size = value, size
		c.ll.MoveToFront(el)
	} else {
		c.items[key] = c.ll.PushFront(&cacheEntry{key: key, value: value, size: size})
		c.used += size
	}
	for c.used > c.limit {
		el := c.ll.Back()
		e := el.Value.(*cacheEntry)
		c.ll.Remove(el)
		delete(c.items, e.key)
		c.used -= e.size
	}
}

// bytes returns the estimated memory held.
func (c *sizedCache) bytes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.used
}
