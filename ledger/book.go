package ledger

import (
	"errors"
	"fmt"
)

// Conflict and binding errors raised while acquiring one-shot leases and while
// sealing basket samples. The store surfaces these as stable API codes.
var (
	// ErrConflict reports a resource already held by another open task.
	ErrConflict = errors.New("ledger: resource conflict")
	// ErrDuplicateBlindCode reports a blind code reused across samples.
	ErrDuplicateBlindCode = errors.New("ledger: duplicate blind code")
	// ErrBlindMismatch reports a reveal that does not match the sealed sample.
	ErrBlindMismatch = errors.New("ledger: blind code mismatch")
)

// ValidateSealBinding checks that a set of basket-seal/bind-code bindings is
// one-to-one and free of duplicate seals or blind codes before the atomic
// sample-seal write (domain rule 4).
func ValidateSealBinding(bindings []SealBinding) error {
	seals := make(map[string]struct{}, len(bindings))
	codes := make(map[string]struct{}, len(bindings))
	for _, b := range bindings {
		if _, ok := seals[b.BasketSeal]; ok {
			return fmt.Errorf("%w: seal %q", ErrConflict, b.BasketSeal)
		}
		if _, ok := codes[b.BlindCode]; ok {
			return fmt.Errorf("%w: %q", ErrDuplicateBlindCode, b.BlindCode)
		}
		seals[b.BasketSeal] = struct{}{}
		codes[b.BlindCode] = struct{}{}
	}
	return nil
}

// SealBinding pairs one basket seal with one blind code at sample-seal time.
type SealBinding struct {
	BasketSeal string
	BlindCode  string
}

// IdentityLeases returns the identity resource leases acquired at lock time:
// the cut-seed batch number plus every basket seal and blind code. These freeze
// the batch identity so no other open task may reuse the same identifiers.
func IdentityLeases(batch string, seals, codes []string) []Lease {
	var out []Lease
	out = append(out, Lease{ResourceType: ResourceBatchNumber, ResourceKey: ResourceKey(batch)})
	for _, s := range seals {
		out = append(out, Lease{ResourceType: ResourceBasketSeal, ResourceKey: ResourceKey(s)})
	}
	for _, c := range codes {
		out = append(out, Lease{ResourceType: ResourceBlindCode, ResourceKey: ResourceKey(c)})
	}
	return out
}

// PhysicalLeases returns the physical resource leases occupied at sample-seal
// time: the healing shed, probe window, sprout-room slot and every test well.
// These are frozen at lock but only occupied once samples are sealed.
func PhysicalLeases(shed, probe, slot string, wells []string) []Lease {
	var out []Lease
	out = append(out, Lease{ResourceType: ResourceHealingShed, ResourceKey: ResourceKey(shed)})
	out = append(out, Lease{ResourceType: ResourceProbeWindow, ResourceKey: ResourceKey(probe)})
	out = append(out, Lease{ResourceType: ResourceSproutSlot, ResourceKey: ResourceKey(slot)})
	for _, w := range wells {
		out = append(out, Lease{ResourceType: ResourceTestWell, ResourceKey: ResourceKey(w)})
	}
	return out
}
