package pages

import "sync"

// Cache for pages

type TmplCache[K comparable, V any] struct {
	data  map[K]V
	mutex sync.RWMutex
}

func NewTmplCache[K comparable, V any]() *TmplCache[K, V] {
	return &TmplCache[K, V]{
		data: make(map[K]V),
	}
}

func (c *TmplCache[K, V]) Get(key K) (V, bool) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	val, exists := c.data[key]
	return val, exists
}

func (c *TmplCache[K, V]) Set(key K, value V) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.data[key] = value
}

func (c *TmplCache[K, V]) Size() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	return len(c.data)
}
