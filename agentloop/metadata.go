package agentloop

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

// Append appends items to the []T slice stored at key in a MetadataStore.
// If the key does not exist, a new slice is created. Panics if the existing value
// is not of type []T.
func Append[T any](m *MetadataStore, key string, items ...T) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.data[key]
	if !ok {
		m.data[key] = append([]T(nil), items...)
		return
	}
	m.data[key] = append(existing.([]T), items...)
}

// MetadataView provides lock-free Get, Set, and Delete access to a MetadataStore.
// It is only valid for the duration of a RunWithLock callback.
type MetadataView interface {
	Set(key string, value any)
	Get(key string) (any, bool)
	Delete(key string)
}

type metadataView struct {
	data map[string]any
}

func (v metadataView) Set(key string, value any)  { v.data[key] = value }
func (v metadataView) Get(key string) (any, bool) { val, ok := v.data[key]; return val, ok }
func (v metadataView) Delete(key string)          { delete(v.data, key) }

// RunWithLock acquires the write lock and calls fn with a lock-free MetadataView.
// Use this when multiple Get/Set/Delete operations must execute atomically.
func (m *MetadataStore) RunWithLock(fn func(m MetadataView)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(metadataView{data: m.data})
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
