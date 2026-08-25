package task

import "testing"

func TestStateNames(t *testing.T) {
	want := map[State]string{
		StatePendingLock:   "pending_lock",
		StateReadyToSprout: "ready_to_sprout",
		StateSprouted:      "sprouted",
	}
	for s, name := range want {
		if got := s.String(); got != name {
			t.Errorf("String(%d) = %q; want %q", s, got, name)
		}
	}
}

func TestTerminal(t *testing.T) {
	for _, s := range []State{StateSprouted, StateDiseaseQuarantine, StateCancelled} {
		if !s.Terminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
	if StateReadyToSprout.Terminal() {
		t.Errorf("ready_to_sprout must not be terminal")
	}
}

func TestCanTransition(t *testing.T) {
	// Happy path one-step advance.
	if !StatePendingLock.CanTransition(StatePendingCutConfirm) {
		t.Errorf("pending_lock should advance to pending_cut_confirm")
	}
	// Terminal outcome from any open state.
	if !StateHealingObserving.CanTransition(StateDiseaseQuarantine) {
		t.Errorf("healing_observing should allow disease_quarantine")
	}
	// Skips are illegal.
	if StatePendingLock.CanTransition(StateHealingObserving) {
		t.Errorf("pending_lock must not skip to healing_observing")
	}
	// Terminal states admit no transitions.
	if StateSprouted.CanTransition(StateCancelled) {
		t.Errorf("sprouted must not transition")
	}
}
