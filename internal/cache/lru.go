package cache

import "container/list"

// LRU is a generic O(1) LRU cache backed by a doubly-linked list.
// It is NOT safe for concurrent use; callers must hold their own mutex.
// Zero value is invalid; use NewLRU.
type LRU[K comparable, V any] struct {
	entries map[K]*list.Element
	order   *list.List
	maxSize int
}

type lruEntry[K comparable, V any] struct {
	key   K
	value V
}

// NewLRU creates an LRU cache with the given maximum capacity.
// maxSize must be > 0.
func NewLRU[K comparable, V any](maxSize int) *LRU[K, V] {
	return &LRU[K, V]{
		entries: make(map[K]*list.Element, maxSize),
		order:   list.New(),
		maxSize: maxSize,
	}
}

// Get returns the value for key and true, or the zero value and false if absent.
// Accessing a key moves it to the most-recently-used position.
func (c *LRU[K, V]) Get(key K) (V, bool) {
	el, ok := c.entries[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*lruEntry[K, V]).value, true
}

// Set inserts or updates key with value, evicting the LRU entry if at capacity.
func (c *LRU[K, V]) Set(key K, value V) {
	if el, ok := c.entries[key]; ok {
		c.order.MoveToFront(el)
		el.Value.(*lruEntry[K, V]).value = value
		return
	}
	if c.order.Len() >= c.maxSize {
		tail := c.order.Back()
		if tail != nil {
			evicted := c.order.Remove(tail).(*lruEntry[K, V])
			delete(c.entries, evicted.key)
		}
	}
	entry := &lruEntry[K, V]{key: key, value: value}
	el := c.order.PushFront(entry)
	c.entries[key] = el
}

// Delete removes key from the cache. No-op if absent.
func (c *LRU[K, V]) Delete(key K) {
	if el, ok := c.entries[key]; ok {
		c.order.Remove(el)
		delete(c.entries, key)
	}
}

// Len returns the number of entries currently in the cache.
func (c *LRU[K, V]) Len() int {
	return c.order.Len()
}
