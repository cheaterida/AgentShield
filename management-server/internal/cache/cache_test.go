package cache

import (
	"testing"
)

type testStruct struct {
	Name  string
	Count int
}

func TestMarshalRoundtrip(t *testing.T) {
	original := testStruct{Name: "hello", Count: 42}
	data, err := Marshal(&original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var restored testStruct
	if err := Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if restored.Name != original.Name || restored.Count != original.Count {
		t.Errorf("roundtrip mismatch: got %+v, want %+v", restored, original)
	}
}

func TestMarshalNil(t *testing.T) {
	data, err := Marshal(nil)
	if err != nil {
		t.Fatalf("Marshal nil failed: %v", err)
	}
	if string(data) != "null" {
		t.Errorf("Marshal nil: got %s, want null", string(data))
	}
}
