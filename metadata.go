/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package ttrpc

import (
	"context"
	"strings"
	"sync"
)

// MD is the user type for ttrpc metadata
type MD struct {
	data map[string][]string
	mu   sync.RWMutex
}

// NewMD creates a metadata object.
func NewMD(data map[string][]string) *MD {
	return &MD{
		data: data,
	}
}

// Get returns the metadata for a given key when they exist.
// If there is no metadata, a nil slice and false are returned.
func (m *MD) Get(key string) ([]string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key = strings.ToLower(key)
	list, ok := m.data[key]
	if !ok || len(list) == 0 {
		return nil, false
	}

	return list, true
}

// Set sets the provided values for a given key.
// The values will overwrite any existing values.
// If no values provided, a key will be deleted.
func (m *MD) Set(key string, values ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key = strings.ToLower(key)
	if len(values) == 0 {
		delete(m.data, key)
		return
	}
	m.data[key] = values
}

// Append appends additional values to the given key.
func (m *MD) Append(key string, values ...string) {
	key = strings.ToLower(key)
	if len(values) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	current, ok := m.data[key]
	if ok {
		m.data[key] = append(current, values...)
	} else {
		m.data[key] = values
	}
}

// GetCopy returns the metadata for a given key when they exist.
func (m *MD) GetCopy() map[string][]string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mCopy := make(map[string][]string, len(m.data))
	for key, value := range m.data {
		mCopy[key] = value
	}

	return mCopy
}

func (m *MD) setRequest(r *Request) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for k, values := range m.data {
		for _, v := range values {
			r.Metadata = append(r.Metadata, &KeyValue{
				Key:   k,
				Value: v,
			})
		}
	}
}

func (m *MD) fromRequest(r *Request) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, kv := range r.Metadata {
		m.data[kv.Key] = append(m.data[kv.Key], kv.Value)
	}
}

type metadataKey struct{}

// GetMetadata retrieves metadata from context.Context (previously attached with WithMetadata)
func GetMetadata(ctx context.Context) (*MD, bool) {
	metadata, ok := ctx.Value(metadataKey{}).(*MD)
	return metadata, ok
}

// GetMetadataValue gets a specific metadata value by name from context.Context
func GetMetadataValue(ctx context.Context, name string) (string, bool) {
	metadata, ok := GetMetadata(ctx)
	if !ok {
		return "", false
	}

	if list, ok := metadata.Get(name); ok {
		return list[0], true
	}

	return "", false
}

// WithMetadata attaches metadata map to a context.Context
func WithMetadata(ctx context.Context, md *MD) context.Context {
	return context.WithValue(ctx, metadataKey{}, md)
}
