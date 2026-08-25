package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"potatoeye-cutseed-sprout-gate/task"
)

func callByAdapter(calls []AdapterCall, adapter string) (AdapterCall, bool) {
	for _, c := range calls {
		if c.Adapter == adapter {
			return c, true
		}
	}
	return AdapterCall{}, false
}

func TestProbeTimeoutFormsRetry(t *testing.T) {
	s := newMemStore(t)
	id, gen := advanceToObserve(t, s)
	s.InjectFault("probe:probe-1", ErrAdapterTimeout)

	_, err := s.Observe(context.Background(), id, 1, observeReq(gen, observeCells()))
	if err == nil {
		t.Fatal("probe timeout must surface as an adapter error")
	}
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeAdapterRetry {
		t.Fatalf("err = %v; want ADAPTER_RETRY", err)
	}

	calls, err := loadAdapterCalls(context.Background(), s.db, id)
	if err != nil {
		t.Fatalf("load adapter calls: %v", err)
	}
	if c, ok := callByAdapter(calls, adapterProbe); !ok || c.Status != "timeout" {
		t.Fatalf("probe call = %+v; want a timeout call", c)
	}

	obs, err := loadObservations(context.Background(), s.db, id)
	if err != nil {
		t.Fatalf("load observations: %v", err)
	}
	if len(obs) != 0 {
		t.Fatalf("probe failure wrote %d observations; want 0", len(obs))
	}
}

func TestPlateReaderMalformedNoEvidence(t *testing.T) {
	s := newMemStore(t)
	id, gen := advanceToObserve(t, s)
	if _, err := s.Observe(context.Background(), id, 1, observeReq(gen, observeCells())); err != nil {
		t.Fatalf("observe: %v", err)
	}
	gen = currentGen(t, s, id)
	if _, err := s.VerifyBudEyes(context.Background(), id, BudEyeRequest{Generation: gen, Counts: budCounts()}); err != nil {
		t.Fatalf("bud eyes: %v", err)
	}
	gen = currentGen(t, s, id)

	s.InjectFault("plate_reader:well-1", ErrAdapterMalformed)
	_, err := s.AddDiseaseEvidence(context.Background(), id, DiseaseRequest{
		Generation: gen, BlindCode: "code-1", WellID: "well-1",
	})
	if err == nil {
		t.Fatal("malformed plate reader must surface as an adapter error")
	}
	var se *Error
	if !errors.As(err, &se) || se.Code != CodeAdapterRetry {
		t.Fatalf("err = %v; want ADAPTER_RETRY", err)
	}
	disease, err := loadDisease(context.Background(), s.db, id)
	if err != nil {
		t.Fatalf("load disease: %v", err)
	}
	if len(disease) != 0 {
		t.Fatalf("malformed plate reader wrote %d disease rows; want 0", len(disease))
	}
}

func TestScaleDisconnectRetriedByClock(t *testing.T) {
	s := newMemStore(t)
	id, gen := advanceToObserve(t, s)

	s.InjectFault("scale:B-1", ErrAdapterDisconnected)
	if _, err := s.Observe(context.Background(), id, 1, observeReq(gen, observeCells())); err == nil {
		t.Fatal("scale disconnect must surface as an adapter error")
	}

	s.ClearFault("scale:B-1")
	// Same generation: the first observe rolled back and left no evidence.
	if _, err := s.Observe(context.Background(), id, 1, observeReq(gen, observeCells())); err != nil {
		t.Fatalf("retry observe: %v", err)
	}

	calls, err := loadAdapterCalls(context.Background(), s.db, id)
	if err != nil {
		t.Fatalf("load adapter calls: %v", err)
	}
	var retried AdapterCall
	for _, c := range calls {
		if c.Adapter == adapterScale && c.Status == "ok" {
			retried = c
		}
	}
	if retried.RetryCount != 1 {
		t.Fatalf("scale retry_count = %d; want 1", retried.RetryCount)
	}
}

func TestRestartRetryQueueOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	id, gen := advanceToObserve(t, s)
	s.InjectFault("probe:probe-1", ErrAdapterTimeout)
	if _, err := s.Observe(context.Background(), id, 1, observeReq(gen, observeCells())); err == nil {
		t.Fatal("probe timeout must fail")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	calls, err := loadAdapterCalls(context.Background(), s2.db, id)
	if err != nil {
		t.Fatalf("load adapter calls after restart: %v", err)
	}
	if c, ok := callByAdapter(calls, adapterProbe); !ok || c.Status != "timeout" {
		t.Fatalf("probe call after restart = %+v; want timeout preserved", c)
	}
	// The task aggregate itself must also survive restart.
	d, err := s2.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("get task after restart: %v", err)
	}
	if d.Task.State != task.StateOccupyingShed {
		t.Fatalf("state after restart = %s; want occupying_shed", d.Task.State)
	}
}
