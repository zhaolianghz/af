// SPDX-License-Identifier: Apache-2.0 OR AGPL-3.0-or-later
// Tests for Node interface and ParamError.
package orchestrator

import (
	"errors"
	"testing"
)

func TestParamError_Message(t *testing.T) {
	pe := NewParamError("filter", "field", "is required")
	if pe.NodeID != "filter" || pe.Field != "field" || pe.Reason != "is required" {
		t.Errorf("fields: %+v", pe)
	}
	if pe.Error() == "" {
		t.Error("Error() should return non-empty")
	}
	if !contains(pe.Error(), "filter") {
		t.Errorf("Error should mention node id: %q", pe.Error())
	}
	if !contains(pe.Error(), "field") {
		t.Errorf("Error should mention field: %q", pe.Error())
	}
}

func TestParamError_NilSafe(t *testing.T) {
	var pe *ParamError
	if pe.Error() != "" {
		t.Errorf("nil ParamError.Error() should be empty, got %q", pe.Error())
	}
}

func TestParamError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	pe := &ParamError{NodeID: "n", Field: "f", Reason: "r", Wrapped: cause}
	if !errors.Is(pe, cause) {
		t.Error("Unwrap should expose the Wrapped error")
	}
}

func TestNewParamError_WrapsInvalidArg(t *testing.T) {
	pe := NewParamError("n", "f", "r")
	if pe.Wrapped == nil {
		t.Fatal("Wrapped should be non-nil")
	}
}

// contains is a tiny substring helper to avoid importing
// strings in this test file.
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
