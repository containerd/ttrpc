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

func TestHeaders_Get(t *testing.T) {
	hdr := make(Headers)
	hdr.Set("foo", "1", "2")

	if list, ok := hdr.Get("foo"); !ok {
		t.Error("key not found")
	} else if len(list) != 2 {
		t.Errorf("unexpected number of values: %d", len(list))
	} else if list[0] != "1" {
		t.Errorf("invalid header value at 0: %s", list[0])
	} else if list[1] != "2" {
		t.Errorf("invalid header value at 1: %s", list[1])
	}
}

func TestHeaders_GetInvalidKey(t *testing.T) {
	hdr := make(Headers)
	hdr.Set("foo", "1", "2")

	if _, ok := hdr.Get("invalid"); ok {
		t.Error("found invalid key")
	}
}

func TestHeaders_Unset(t *testing.T) {
	hdr := make(Headers)
	hdr.Set("foo", "1", "2")
	hdr.Set("foo")

	if _, ok := hdr.Get("foo"); ok {
		t.Error("key not deleted")
	}
}

func TestHeader_Replace(t *testing.T) {
	hdr := make(Headers)
	hdr.Set("foo", "1", "2")
	hdr.Set("foo", "3", "4")

	if list, ok := hdr.Get("foo"); !ok {
		t.Error("key not found")
	} else if len(list) != 2 {
		t.Errorf("unexpected number of values: %d", len(list))
	} else if list[0] != "3" {
		t.Errorf("invalid header value at 0: %s", list[0])
	} else if list[1] != "4" {
		t.Errorf("invalid header value at 1: %s", list[1])
	}
}

func TestHeaders_Append(t *testing.T) {
	hdr := make(Headers)
	hdr.Set("foo", "1")
	hdr.Append("foo", "2")
	hdr.Append("bar", "3")

	if list, ok := hdr.Get("foo"); !ok {
		t.Error("key not found")
	} else if len(list) != 2 {
		t.Errorf("unexpected number of values: %d", len(list))
	} else if list[0] != "1" {
		t.Errorf("invalid header value at 0: %s", list[0])
	} else if list[1] != "2" {
		t.Errorf("invalid header value at 1: %s", list[1])
	}

	if list, ok := hdr.Get("bar"); !ok {
		t.Error("key not found")
	} else if list[0] != "3" {
		t.Errorf("invalid value: %s", list[0])
	}
}

func TestHeaders_Context(t *testing.T) {
	hdr := make(Headers)
	hdr.Set("foo", "bar")

	ctx := WithHeaders(context.Background(), hdr)

	if bar, ok := GetHeader(ctx, "foo"); !ok {
		t.Error("header not found")
	} else if bar != "bar" {
		t.Errorf("invalid header value: %q", bar)
	}
}
