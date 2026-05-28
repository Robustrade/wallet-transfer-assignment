package cache

import (
	"container/list"
	"sync"
	"time"
)

type Option func(*options)

type options struct {
	maxEntries int
	defaultTTL time.Duration
}

func WithMaxEntries(n int) Option {
	return func(o *options) { o.maxEntries = n }
}

func WithDefaultTTL(d time.Duration) Option {
	return func(o *options) { o.defaultTTL = d }
}

type entry[K comparable, V any] struct {
	key      K
	value    V
	expires  time.Time
	hasTTL   bool
	listElem *list.Element
}

// LRUTTL is a small in-memory cache with max size + per-entry TTL.
// It is best-effort only and must not be used as a source of truth.
type LRUTTL[K comparable, V any] struct {
	mu         sync.Mutex
	items      map[K]*entry[K, V]
	order      *list.List
	maxEntries int
	defaultTTL time.Duration
}

func NewLRUTTL[K comparable, V any](opts ...Option) *LRUTTL[K, V] {
	o := options{
		maxEntries: 10_000,
		defaultTTL: 10 * time.Minute,
	}
	for _, opt := range opts {
		opt(&o)
	}
	return &LRUTTL[K, V]{
		items:      make(map[K]*entry[K, V]),
		order:      list.New(),
		maxEntries: o.maxEntries,
		defaultTTL: o.defaultTTL,
	}
}

func (c *LRUTTL[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.items[key]; ok {
		if e.hasTTL && time.Now().After(e.expires) {
			c.removeLocked(e)
			var zero V
			return zero, false
		}
		c.order.MoveToFront(e.listElem)
		return e.value, true
	}

	var zero V
	return zero, false
}

func (c *LRUTTL[K, V]) Set(key K, value V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ttl <= 0 {
		ttl = c.defaultTTL
	}
	if e, ok := c.items[key]; ok {
		e.value = value
		e.hasTTL = true
		e.expires = time.Now().Add(ttl)
		c.order.MoveToFront(e.listElem)
		return
	}

	elem := c.order.PushFront(key)
	e := &entry[K, V]{
		key:      key,
		value:    value,
		hasTTL:   true,
		expires:  time.Now().Add(ttl),
		listElem: elem,
	}
	c.items[key] = e

	for c.maxEntries > 0 && c.order.Len() > c.maxEntries {
		last := c.order.Back()
		if last == nil {
			break
		}
		lastKey := last.Value.(K)
		c.removeByKeyLocked(lastKey)
	}
}

func (c *LRUTTL[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.removeByKeyLocked(key)
}

func (c *LRUTTL[K, V]) removeByKeyLocked(key K) {
	if e, ok := c.items[key]; ok {
		c.removeLocked(e)
	}
}

func (c *LRUTTL[K, V]) removeLocked(e *entry[K, V]) {
	delete(c.items, e.key)
	c.order.Remove(e.listElem)
}
