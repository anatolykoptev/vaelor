package main

import (
	"reflect"
	"testing"
)

// TestFieldAccessDescParity verifies that UnderstandInput.FieldAccess and
// CallTraceInput.FieldAccess carry identical jsonschema_description tags and
// that both equal the fieldAccessDesc const. Prevents silent drift between the
// two tool schemas.
func TestFieldAccessDescParity(t *testing.T) {
	understandTag := reflect.TypeOf(UnderstandInput{}).
		Field(indexOf(t, reflect.TypeOf(UnderstandInput{}), "FieldAccess")).
		Tag.Get("jsonschema_description")

	callTraceTag := reflect.TypeOf(CallTraceInput{}).
		Field(indexOf(t, reflect.TypeOf(CallTraceInput{}), "FieldAccess")).
		Tag.Get("jsonschema_description")

	if understandTag != fieldAccessDesc {
		t.Errorf("UnderstandInput.FieldAccess jsonschema_description does not match fieldAccessDesc\ngot:  %q\nwant: %q",
			understandTag, fieldAccessDesc)
	}
	if callTraceTag != fieldAccessDesc {
		t.Errorf("CallTraceInput.FieldAccess jsonschema_description does not match fieldAccessDesc\ngot:  %q\nwant: %q",
			callTraceTag, fieldAccessDesc)
	}
}

// indexOf returns the struct field index for the given field name.
// Uses t.Fatalf (not panic) so a missing field produces a proper FAIL line
// and does not crash the entire test binary.
func indexOf(t *testing.T, rt reflect.Type, name string) int {
	t.Helper()
	for i := range rt.NumField() {
		if rt.Field(i).Name == name {
			return i
		}
	}
	t.Fatalf("field %q not found in %v", name, rt)
	return -1
}
