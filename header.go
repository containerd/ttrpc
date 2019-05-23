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

import "context"

// Headers represents the key-value pairs (similar to http.Header) to be passed to ttrpc server from a client.
type Headers map[string]StringList

// Get returns the headers for a given key when they exist.
// If there are no headers, a nil slice and false are returned.
func (h Headers) Get(key string) ([]string, bool) {
	list, ok := h[key]
	if !ok || len(list.List) == 0 {
		return nil, false
	}

	return list.List, true
}

// Set sets the provided values for a given key.
// The values will overwrite any existing values.
// If no values provided, a key will be deleted.
func (h Headers) Set(key string, values ...string) {
	if len(values) == 0 {
		delete(h, key)
		return
	}

	h[key] = StringList{List: values}
}

// Append appends additional values to the given key.
func (h Headers) Append(key string, values ...string) {
	if len(values) == 0 {
		return
	}

	list, ok := h[key]
	if ok {
		h.Set(key, append(list.List, values...)...)
	} else {
		h.Set(key, values...)
	}
}

type headerKey struct{}

// GetHeaders retrieves headers from context.Context (previously attached with WithHeaders)
func GetHeaders(ctx context.Context) (Headers, bool) {
	headers, ok := ctx.Value(headerKey{}).(Headers)
	return headers, ok
}

// GetHeader gets a specific header value by name from context.Context
func GetHeader(ctx context.Context, name string) (string, bool) {
	headers, ok := GetHeaders(ctx)
	if !ok {
		return "", false
	}

	if list, ok := headers.Get(name); ok {
		return list[0], true
	}

	return "", false
}

// WithHeaders attaches headers map to a context.Context
func WithHeaders(ctx context.Context, headers Headers) context.Context {
	return context.WithValue(ctx, headerKey{}, headers)
}
