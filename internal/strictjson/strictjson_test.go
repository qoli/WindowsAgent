package strictjson

import "testing"

func TestValidateRejectsNestedDuplicateKey(t *testing.T) {
	if err := Validate([]byte(`{"outer":{"value":1,"value":2}}`)); err == nil {
		t.Fatal("duplicate key was accepted")
	}
}

func TestValidateAcceptsDistinctKeysAtDifferentLevels(t *testing.T) {
	if err := Validate([]byte(`{"value":1,"outer":{"value":2}}`)); err != nil {
		t.Fatal(err)
	}
}
