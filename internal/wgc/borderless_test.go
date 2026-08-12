package wgc

import "testing"

func TestBorderlessAccessRejectsEveryNonAllowedStatus(t *testing.T) {
	for status := int32(0); status <= 4; status++ {
		err := requireBorderlessAccess(status)
		if status == borderlessAccessAllowed && err != nil {
			t.Fatalf("allowed status rejected: %v", err)
		}
		if status != borderlessAccessAllowed && err == nil {
			t.Fatalf("status %d unexpectedly allowed", status)
		}
	}
}

func TestBorderSettingRequiresExactReadback(t *testing.T) {
	if err := requireBorderSetting(false, false); err != nil {
		t.Fatal(err)
	}
	if err := requireBorderSetting(false, true); err == nil {
		t.Fatal("visible border unexpectedly satisfied the borderless contract")
	}
}
