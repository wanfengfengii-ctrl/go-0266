package ledger

import "testing"

func TestResourceTypeValid(t *testing.T) {
	for _, rt := range []ResourceType{
		ResourceBatchNumber, ResourceBasketSeal, ResourceBlindCode,
		ResourceHealingShed, ResourceProbeWindow, ResourceSproutSlot, ResourceTestWell,
	} {
		if !rt.Valid() {
			t.Errorf("%s should be valid", rt)
		}
	}
	if ResourceType("nonsense").Valid() {
		t.Errorf("unknown resource type must be invalid")
	}
}

func TestLeaseKeyUnique(t *testing.T) {
	a := LeaseKey(ResourceHealingShed, "shed-1")
	b := LeaseKey(ResourceHealingShed, "shed-2")
	c := LeaseKey(ResourceProbeWindow, "shed-1")
	if a == b || a == c || b == c {
		t.Fatalf("distinct resources must have distinct lease keys")
	}
}
