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
	"fmt"
	"sync"
	"testing"
)

func TestMetadataGet(t *testing.T) {
	metadata := make(MD)
	metadata.Set("foo", "1", "2")

	if list, ok := metadata.Get("foo"); !ok {
		t.Error("key not found")
	} else if len(list) != 2 {
		t.Errorf("unexpected number of values: %d", len(list))
	} else if list[0] != "1" {
		t.Errorf("invalid metadata value at 0: %s", list[0])
	} else if list[1] != "2" {
		t.Errorf("invalid metadata value at 1: %s", list[1])
	}
}

func TestMetadataGetInvalidKey(t *testing.T) {
	metadata := make(MD)
	metadata.Set("foo", "1", "2")

	if _, ok := metadata.Get("invalid"); ok {
		t.Error("found invalid key")
	}
}

func TestMetadataUnset(t *testing.T) {
	metadata := make(MD)
	metadata.Set("foo", "1", "2")
	metadata.Set("foo")

	if _, ok := metadata.Get("foo"); ok {
		t.Error("key not deleted")
	}
}

func TestMetadataReplace(t *testing.T) {
	metadata := make(MD)
	metadata.Set("foo", "1", "2")
	metadata.Set("foo", "3", "4")

	if list, ok := metadata.Get("foo"); !ok {
		t.Error("key not found")
	} else if len(list) != 2 {
		t.Errorf("unexpected number of values: %d", len(list))
	} else if list[0] != "3" {
		t.Errorf("invalid metadata value at 0: %s", list[0])
	} else if list[1] != "4" {
		t.Errorf("invalid metadata value at 1: %s", list[1])
	}
}

func TestMetadataAppend(t *testing.T) {
	metadata := make(MD)
	metadata.Set("foo", "1")
	metadata.Append("foo", "2")
	metadata.Append("bar", "3")

	if list, ok := metadata.Get("foo"); !ok {
		t.Error("key not found")
	} else if len(list) != 2 {
		t.Errorf("unexpected number of values: %d", len(list))
	} else if list[0] != "1" {
		t.Errorf("invalid metadata value at 0: %s", list[0])
	} else if list[1] != "2" {
		t.Errorf("invalid metadata value at 1: %s", list[1])
	}

	if list, ok := metadata.Get("bar"); !ok {
		t.Error("key not found")
	} else if list[0] != "3" {
		t.Errorf("invalid value: %s", list[0])
	}
}

func TestMetadataContext(t *testing.T) {
	metadata := make(MD)
	metadata.Set("foo", "bar")

	ctx := WithMetadata(context.Background(), metadata)

	if bar, ok := GetMetadataValue(ctx, "foo"); !ok {
		t.Error("metadata not found")
	} else if bar != "bar" {
		t.Errorf("invalid metadata value: %q", bar)
	}
}

func TestMetadataClone(t *testing.T) {
	var metadata MD
	m2 := metadata.Clone()
	if m2 != nil {
		t.Error("MD.Clone() on nil metadata should return nil")
	}

	metadata = MD{"nil": nil, "foo": {"bar"}, "baz": {"qux", "quxx"}}
	m2 = metadata.Clone()

	if len(metadata) != len(m2) {
		t.Errorf("unexpected number of keys: %d, expected: %d", len(m2), len(metadata))
	}

	for k, v := range metadata {
		v2, ok := m2[k]
		if !ok {
			t.Errorf("key not found: %s", k)
		}
		if v == nil && v2 == nil {
			continue
		}
		if v == nil || v2 == nil {
			t.Errorf("unexpected nil value: %v, expected: %v", v2, v)
		}
		if len(v) != len(v2) {
			t.Errorf("unexpected number of values: %d, expected: %d", len(v2), len(v))
		}
		for i := range v {
			if v[i] != v2[i] {
				t.Errorf("unexpected value: %s, expected: %s", v2[i], v[i])
			}
		}
	}
}

func TestMetadataCloneConcurrent(t *testing.T) {
	metadata := make(MD)
	metadata.Set("foo", "bar")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m2 := metadata.Clone()
			m2.Set("foo", "baz")
		}()
	}
	wg.Wait()
	// Concurrent modification should clone the metadata first to avoid panic
	// due to concurrent map writes.
	if val, ok := metadata.Get("foo"); !ok {
		t.Error("metadata not found")
	} else if val[0] != "bar" {
		t.Errorf("invalid metadata value: %q", val[0])
	}
}

func simpleClone(src MD) MD {
	md := MD{}
	for k, v := range src {
		md[k] = append(md[k], v...)
	}
	return md
}

func BenchmarkMetadataClone(b *testing.B) {
	for _, sz := range []int{5, 10, 20, 50} {
		b.Run(fmt.Sprintf("size=%d", sz), func(b *testing.B) {
			metadata := make(MD)
			for i := 0; i < sz; i++ {
				metadata.Set("foo"+fmt.Sprint(i), "bar"+fmt.Sprint(i))
			}

			for i := 0; i < b.N; i++ {
				_ = metadata.Clone()
			}
		})
	}
}

func BenchmarkSimpleMetadataClone(b *testing.B) {
	for _, sz := range []int{5, 10, 20, 50} {
		b.Run(fmt.Sprintf("size=%d", sz), func(b *testing.B) {
			metadata := make(MD)
			for i := 0; i < sz; i++ {
				metadata.Set("foo"+fmt.Sprint(i), "bar"+fmt.Sprint(i))
			}

			for i := 0; i < b.N; i++ {
				_ = simpleClone(metadata)
			}
		})
	}
}
