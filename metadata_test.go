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
	"testing"
)

func TestMetadata_Get(t *testing.T) {
	metadata := make(Metadata)
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

func TestMetadata_GetInvalidKey(t *testing.T) {
	metadata := make(Metadata)
	metadata.Set("foo", "1", "2")

	if _, ok := metadata.Get("invalid"); ok {
		t.Error("found invalid key")
	}
}

func TestMetadata_Unset(t *testing.T) {
	metadata := make(Metadata)
	metadata.Set("foo", "1", "2")
	metadata.Set("foo")

	if _, ok := metadata.Get("foo"); ok {
		t.Error("key not deleted")
	}
}

func TestMetadata_Replace(t *testing.T) {
	metadata := make(Metadata)
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

func TestMetadata_Append(t *testing.T) {
	metadata := make(Metadata)
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

func TestMetadata_Context(t *testing.T) {
	metadata := make(Metadata)
	metadata.Set("foo", "bar")

	ctx := WithMetadata(context.Background(), metadata)

	if bar, ok := GetMetadataValue(ctx, "foo"); !ok {
		t.Error("metadata not found")
	} else if bar != "bar" {
		t.Errorf("invalid metadata value: %q", bar)
	}
}
