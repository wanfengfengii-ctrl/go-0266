package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"potatoeye-cutseed-sprout-gate/api"
	"potatoeye-cutseed-sprout-gate/store"
)

func TestModel_LockRetryReplaysOriginalResultAndKeepsConflicts(t *testing.T) {
	type lockResponse struct {
		status int
		lock   store.LockResult
		err    api.ErrorResponse
		body   string
	}

	newFixture := func(t *testing.T) (*store.SQLite, http.Handler) {
		t.Helper()
		st, err := store.OpenMemory()
		if err != nil {
			t.Fatalf("open memory store: %v", err)
		}
		t.Cleanup(func() { _ = st.Close() })
		return st, api.NewServer(st).Handler()
	}
	postLock := func(t *testing.T, h http.Handler, body string) lockResponse {
		t.Helper()
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/v1/tasks/lock", strings.NewReader(body)))
		out := lockResponse{status: rr.Code, body: rr.Body.String()}
		if rr.Code >= http.StatusBadRequest {
			if err := json.NewDecoder(rr.Body).Decode(&out.err); err != nil {
				t.Fatalf("decode error response: %v; body %s", err, out.body)
			}
			return out
		}
		if err := json.NewDecoder(rr.Body).Decode(&out.lock); err != nil {
			t.Fatalf("decode lock response: %v; body %s", err, out.body)
		}
		return out
	}
	lockBody := func(operationID, batch, sealA, sealB, codeA, codeB, shed, probe, slot, well string) string {
		return `{"operation_id":"` + operationID + `","plot":"P-01","variety":"V-G2","cellar":"C-01","disinfect":"D-01",` +
			`"batch":"` + batch + `","basket_seals":["` + sealA + `","` + sealB + `"],"blind_codes":["` + codeA + `","` + codeB + `"],` +
			`"healing_shed":"` + shed + `","probe_window":"` + probe + `","sprout_slot":"` + slot + `",` +
			`"test_wells":["` + well + `"],"points":["p-1","p-2"],"reviewers":["reviewer-1","reviewer-2"]}`
	}
	expectConflict := func(t *testing.T, got lockResponse) {
		t.Helper()
		if got.status != http.StatusConflict || got.err.Code != store.CodeConflict {
			t.Fatalf("lock status/code = %d/%s; want 409/%s: %s", got.status, got.err.Code, store.CodeConflict, got.body)
		}
	}

	cases := []struct {
		name string
		run  func(t *testing.T, st *store.SQLite, h http.Handler)
	}{
		{
			name: "same operation and identical body replays the first lock",
			run: func(t *testing.T, st *store.SQLite, h http.Handler) {
				body := lockBody("op-lock-timeout-retry", "B-1", "seal-1", "seal-2", "code-1", "code-2", "shed-1", "probe-1", "slot-1", "well-1")
				first := postLock(t, h, body)
				if first.status != http.StatusCreated {
					t.Fatalf("first lock status = %d; want 201: %s", first.status, first.body)
				}
				retry := postLock(t, h, body)
				if retry.status != http.StatusCreated {
					t.Fatalf("retry lock status = %d; want 201 replay: %s", retry.status, retry.body)
				}
				if !reflect.DeepEqual(retry.lock, first.lock) {
					t.Fatalf("retry result = %+v; want original %+v", retry.lock, first.lock)
				}
				events, err := st.Audit(httptest.NewRequest(http.MethodGet, "/", nil).Context(), first.lock.TaskID)
				if err != nil {
					t.Fatalf("audit first task: %v", err)
				}
				if len(events) != 1 {
					t.Fatalf("retry wrote %d audit events for the locked task; want only the original lock event", len(events))
				}
			},
		},
		{
			name: "same operation with different content is a stable conflict",
			run: func(t *testing.T, _ *store.SQLite, h http.Handler) {
				firstBody := lockBody("op-lock-different-content", "B-1", "seal-1", "seal-2", "code-1", "code-2", "shed-1", "probe-1", "slot-1", "well-1")
				if got := postLock(t, h, firstBody); got.status != http.StatusCreated {
					t.Fatalf("first lock status = %d; want 201: %s", got.status, got.body)
				}
				changedBody := lockBody("op-lock-different-content", "B-2", "seal-3", "seal-4", "code-3", "code-4", "shed-2", "probe-2", "slot-2", "well-2")
				changed := postLock(t, h, changedBody)
				expectConflict(t, changed)
				again := postLock(t, h, changedBody)
				if again.status != changed.status || again.err.Code != changed.err.Code || again.err.Message != changed.err.Message {
					t.Fatalf("changed-content conflict changed from %d/%s/%q to %d/%s/%q", changed.status, changed.err.Code, changed.err.Message, again.status, again.err.Code, again.err.Message)
				}
			},
		},
		{
			name: "different operation with a real resource collision still conflicts",
			run: func(t *testing.T, _ *store.SQLite, h http.Handler) {
				firstBody := lockBody("op-lock-real-owner", "B-1", "seal-1", "seal-2", "code-1", "code-2", "shed-1", "probe-1", "slot-1", "well-1")
				if got := postLock(t, h, firstBody); got.status != http.StatusCreated {
					t.Fatalf("first lock status = %d; want 201: %s", got.status, got.body)
				}
				conflictBody := lockBody("op-lock-real-conflict", "B-2", "seal-1", "seal-4", "code-3", "code-4", "shed-2", "probe-2", "slot-2", "well-2")
				expectConflict(t, postLock(t, h, conflictBody))
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, h := newFixture(t)
			tc.run(t, st, h)
		})
	}
}
