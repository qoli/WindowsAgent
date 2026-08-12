package frametap

import "testing"

func TestValidateName(t *testing.T) {
	if err := ValidateName(`Local\WindowsAgent.Evidence.EliteDangerous.v1`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Global\\WindowsAgent.Evidence.Game.v1", `Local\Other.Game.v1`, "Local\\WindowsAgent.Evidence.Bad\n.v1"} {
		if err := ValidateName(name); err == nil {
			t.Fatalf("name %q unexpectedly valid", name)
		}
	}
}

func TestPublisherOwnershipNameIsSeparateFromMapping(t *testing.T) {
	name := `Local\WindowsAgent.Evidence.EliteDangerous.v1`
	if got := publisherOwnershipName(name); got != name+".Publisher" {
		t.Fatalf("publisher ownership name = %q", got)
	}
}
