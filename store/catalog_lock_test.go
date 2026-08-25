package store

import (
	"context"
	"errors"
	"testing"
)

func TestLockSuccess(t *testing.T) {
	s := newMemStore(t)
	id, gen := lockDefault(t, s)
	if id == "" {
		t.Fatal("lock must return a task id")
	}
	if gen != 1 {
		t.Fatalf("generation = %d; want 1", gen)
	}
	detail, err := s.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if detail.Task.State.String() != "pending_cut_confirm" {
		t.Fatalf("state = %s; want pending_cut_confirm", detail.Task.State)
	}
}

func TestLockPlotVarietyMismatch(t *testing.T) {
	s := newMemStore(t)
	req := defaultLockRequest()
	req.Variety = "V-G9"
	if _, err := s.Lock(context.Background(), req); err == nil {
		t.Fatal("plot/variety mismatch must be rejected")
	} else {
		var se *Error
		if !errors.As(err, &se) || se.Code != CodeInvalidRequest {
			t.Fatalf("err = %v; want INVALID_REQUEST", err)
		}
	}
}

func TestLockStaleCellar(t *testing.T) {
	s := newMemStore(t)
	req := defaultLockRequest()
	req.Cellar = "C-STALE"
	if _, err := s.Lock(context.Background(), req); err == nil {
		t.Fatal("stale cellar must be rejected")
	} else {
		var se *Error
		if !errors.As(err, &se) || se.Code != CodeConflict {
			t.Fatalf("err = %v; want CONFLICT", err)
		}
	}
}

func TestLockDisinfectMismatch(t *testing.T) {
	s := newMemStore(t)
	req := defaultLockRequest()
	req.Disinfect = "D-STALE"
	if _, err := s.Lock(context.Background(), req); err == nil {
		t.Fatal("disinfect mismatch must be rejected")
	} else {
		var se *Error
		if !errors.As(err, &se) || se.Code != CodeConflict {
			t.Fatalf("err = %v; want CONFLICT", err)
		}
	}
}
