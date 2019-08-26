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
	"testing"
)

func Test_MethodFullNameGeneration(t *testing.T) {
	name := fullPath("test.v1.service", "Foo")
	expectedName := "/test.v1.service/Foo"
	if name != expectedName {
		t.Fatalf("Service name does not match. Expected: %q, Actual: %q", expectedName, name)
	}
}
