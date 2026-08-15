// Copyright 2026 the artemis authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go

import "testing"

func TestSnapshotBlobIsValidRejectsEmptyAndMalformedData(t *testing.T) {
	if SnapshotBlobIsValid(nil) {
		t.Fatal("SnapshotBlobIsValid accepted an empty blob")
	}
	if SnapshotBlobIsValid(make([]byte, 128)) {
		t.Fatal("SnapshotBlobIsValid accepted malformed data")
	}
}
