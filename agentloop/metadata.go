package agentloop

// @author yongj.zhuang

import (
	"sync"
)

// MetadataStore is a concurrency-safe key-value store for sharing arbitrary metadata
// between tools and the invoker across a single agent execution.
//
// Tools write metadata via AgentContext.Metadata during execution; the invoker reads
// the final snapshot from TaskOutput.Metadata after Execute returns.
type MetadataStore struct {
	mu   sync.RWMutex
	data map[string]any
}

// NewMetadataStore creates a new MetadataStore.
func NewMetadataStore() *MetadataStore {
	return &MetadataStore{
		data: make(map[string]any),
	}
}

// Set stores a value under the given key, overwriting any existing value.
func (m *MetadataStore) Set(key string, value any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
}

// Get retrieves the value for the given key.
// Returns the value and true if found, zero value and false otherwise.
func (m *MetadataStore) Get(key string) (any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	return v, ok
}

// Delete removes the entry for the given key. No-op if the key does not exist.
func (m *MetadataStore) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
}

// All returns a shallow copy of all key-value pairs.
func (m *MetadataStore) All() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]any, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out
}

// GetMeta retrieves a typed value from a MetadataStore.
// Returns the typed value and true if the key exists and the value is assignable to T,
// zero value and false otherwise.
func GetMeta[T any](m *MetadataStore, key string) (T, bool) {
	if m == nil {
		var zero T
		return zero, false
	}
	v, ok := m.Get(key)
	if !ok {
		var zero T
		return zero, false
	}
	typed, ok := v.(T)
	return typed, ok
}
