// Package store defines the persistence boundary and the business-flow service
// for the PotatoEye service.
//
// A production implementation persists tasks, leases, idempotency records and
// evidence chains in a SQLite WAL directory so the process can deterministically
// recover after restart (failure boundary 6). The Store composes the component
// boundaries owned by the domain packages and the Service exposes the eight
// documented business flows.
package store

import (
	"context"
	"errors"
	"fmt"

	"potatoeye-cutseed-sprout-gate/arbiter"
	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/evidence"
	"potatoeye-cutseed-sprout-gate/fixed"
	"potatoeye-cutseed-sprout-gate/ledger"
	"potatoeye-cutseed-sprout-gate/task"
)

// Code is a stable error code returned by the service and surfaced verbatim by
// the HTTP API (failure boundary 3).
type Code string

const (
	CodeOK              Code = "OK"
	CodeInvalidRequest  Code = "INVALID_REQUEST"
	CodeNotFound        Code = "NOT_FOUND"
	CodeConflict        Code = "CONFLICT"
	CodeStaleGeneration Code = "STALE_GENERATION"
	CodeTerminal        Code = "TERMINAL_STATE"
	CodeDuplicateKey    Code = "DUPLICATE_KEY"
	CodeAdapterRetry    Code = "ADAPTER_RETRY"
	CodeNotFinalizable  Code = "NOT_FINALIZABLE"
)

// ErrNotFound is the sentinel reported when a task ID is unknown.
var ErrNotFound = errors.New("store: not found")

// Error is a stable, deterministic service error carrying a code and an ordered
// list of reasons sorted by plot, batch, basket seal, day-age and well.
type Error struct {
	Code    Code
	Msg     string
	Reasons []string
}

func (e *Error) Error() string {
	if len(e.Reasons) == 0 {
		return fmt.Sprintf("%s: %s", e.Code, e.Msg)
	}
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Msg, e.Reasons)
}

// Error constructors map domain failures onto stable codes.
func errInvalid(msg string, reasons ...string) *Error {
	return &Error{Code: CodeInvalidRequest, Msg: msg, Reasons: reasons}
}
func errNotFound(msg string) *Error {
	return &Error{Code: CodeNotFound, Msg: msg}
}
func errConflict(msg string, reasons ...string) *Error {
	return &Error{Code: CodeConflict, Msg: msg, Reasons: reasons}
}
func errStale(msg string) *Error {
	return &Error{Code: CodeStaleGeneration, Msg: msg}
}
func errTerminal(msg string) *Error {
	return &Error{Code: CodeTerminal, Msg: msg}
}
func errDuplicate(msg string, reasons ...string) *Error {
	return &Error{Code: CodeDuplicateKey, Msg: msg, Reasons: reasons}
}
func errAdapter(msg string, reasons ...string) *Error {
	return &Error{Code: CodeAdapterRetry, Msg: msg, Reasons: reasons}
}
func errNotFinalizable(msg string, reasons ...string) *Error {
	return &Error{Code: CodeNotFinalizable, Msg: msg, Reasons: reasons}
}

// Service is the business-flow boundary exposed to the HTTP API. Every write
// method runs in a single SQLite transaction (failure boundary 1).
type Service interface {
	Lock(ctx context.Context, req LockRequest) (LockResult, error)
	CutConfirm(ctx context.Context, id task.ID, req CutConfirmRequest) (CutConfirmResult, error)
	SampleSeal(ctx context.Context, id task.ID, req SampleSealRequest) (SampleSealResult, error)
	Observe(ctx context.Context, id task.ID, ageDay evidence.AgeDay, req ObserveRequest) (ObserveResult, error)
	VerifyBudEyes(ctx context.Context, id task.ID, req BudEyeRequest) (BudEyeResult, error)
	AddDiseaseEvidence(ctx context.Context, id task.ID, req DiseaseRequest) (DiseaseResult, error)
	Review(ctx context.Context, id task.ID, req ReviewRequest) (ReviewResult, error)
	FinalizeTask(ctx context.Context, id task.ID, req FinalizeRequest) (FinalizeResult, error)
	GetTask(ctx context.Context, id task.ID) (TaskDetail, error)
	Audit(ctx context.Context, id task.ID) ([]AuditEvent, error)
}

// Store composes the component boundaries and the service behind one handle.
type Store interface {
	Service
	Catalog() catalog.Catalog
	Tasks() task.Aggregate
	Ledger() ledger.Ledger
	Evidence() evidence.Evidence
	Arbiter() arbiter.Arbiter
	Close() error
}

// --- Request/response DTOs (defined once; the API unmarshals into them) ---

// LockRequest freezes every identifier and resource window at lock time.
type LockRequest struct {
	OperationID task.OperationID          `json:"operation_id"`
	Plot        catalog.Plot              `json:"plot"`
	Variety     catalog.VarietyGeneration `json:"variety"`
	Cellar      catalog.CellarSummary     `json:"cellar"`
	Disinfect   catalog.DisinfectBatch    `json:"disinfect"`
	Batch       task.BatchNumber          `json:"batch"`
	BasketSeals []string                  `json:"basket_seals"`
	BlindCodes  []string                  `json:"blind_codes"`
	HealingShed string                    `json:"healing_shed"`
	ProbeWindow string                    `json:"probe_window"`
	SproutSlot  string                    `json:"sprout_slot"`
	TestWells   []string                  `json:"test_wells"`
	Points      []string                  `json:"points"`
	Reviewers   []catalog.PersonnelID     `json:"reviewers"`
}

// LockResult returns the task identity, generation and the acquired lease keys.
type LockResult struct {
	TaskID     task.ID         `json:"task_id"`
	Generation task.Generation `json:"generation"`
	Leases     []string        `json:"leases"`
}

// CutConfirmRequest submits one cutter's confirmation of the frozen snapshot.
type CutConfirmRequest struct {
	OperationID task.OperationID    `json:"operation_id"`
	PersonnelID catalog.PersonnelID `json:"personnel_id"`
	Generation  task.Generation     `json:"generation"`
	Snapshot    task.LockSnapshot   `json:"snapshot"`
}

// CutConfirmResult reports who has confirmed and whether the pair is complete.
type CutConfirmResult struct {
	Confirmed []catalog.PersonnelID `json:"confirmed"`
	Complete  bool                  `json:"complete"`
}

// SampleSealRequest binds basket seals to blind codes and occupies resources.
type SampleSealRequest struct {
	OperationID task.OperationID `json:"operation_id"`
	Generation  task.Generation  `json:"generation"`
	Samples     []SampleBinding  `json:"samples"`
}

// SampleBinding is one seal-to-blind-code binding.
type SampleBinding struct {
	BasketSeal string `json:"basket_seal"`
	BlindCode  string `json:"blind_code"`
}

// SampleSealResult reports the sealed samples.
type SampleSealResult struct {
	Sealed int `json:"sealed"`
}

// ObserveRequest submits one healing day-age coverage matrix plus probe reads.
type ObserveRequest struct {
	OperationID   task.OperationID `json:"operation_id"`
	Generation    task.Generation  `json:"generation"`
	Cells         []ObserveCell    `json:"cells"`
	ShedTemp      fixed.Value      `json:"shed_temp"`
	Humidity      fixed.Value      `json:"humidity"`
	WeightChange  fixed.Value      `json:"weight_change"`
	MissingReason string           `json:"missing_reason,omitempty"`
}

// ObserveCell is one coverage cell of the healing matrix.
type ObserveCell struct {
	BasketSeal        string `json:"basket_seal"`
	PointID           string `json:"point_id"`
	SuberizationGrade int    `json:"suberization_grade"`
	WaterLossBlocks   int    `json:"water_loss_blocks"`
	RotBlocks         int    `json:"rot_blocks"`
	SuberizedBlocks   int    `json:"suberized_blocks"`
	TotalBlocks       int    `json:"total_blocks"`
}

// ObserveResult reports the committed cell count and the recorded probe call.
type ObserveResult struct {
	Cells       int    `json:"cells"`
	ProbeCallID string `json:"probe_call_id,omitempty"`
}

// BudEyeRequest submits bud-eye active counts per coverage point.
type BudEyeRequest struct {
	OperationID task.OperationID `json:"operation_id"`
	Generation  task.Generation  `json:"generation"`
	Counts      []BudEyeCount    `json:"counts"`
}

// BudEyeCount is one point's active/capacity pair.
type BudEyeCount struct {
	BasketSeal string `json:"basket_seal"`
	PointID    string `json:"point_id"`
	Active     int    `json:"active"`
	Capacity   int    `json:"capacity"`
}

// BudEyeResult reports the verified point count.
type BudEyeResult struct {
	Points int `json:"points"`
}

// DiseaseRequest submits a rapid-test plate-well reading for one blind sample.
type DiseaseRequest struct {
	OperationID    task.OperationID `json:"operation_id"`
	Generation     task.Generation  `json:"generation"`
	BlindCode      string           `json:"blind_code"`
	WellID         string           `json:"well_id"`
	LesionDiameter fixed.Value      `json:"lesion_diameter"`
	ExudateLevel   int              `json:"exudate_level"`
	CtValue        fixed.Value      `json:"ct_value"`
}

// DiseaseResult reports the evidence version written and the adapter call.
type DiseaseResult struct {
	Version     evidence.Version `json:"version"`
	AdapterCall string           `json:"adapter_call_id,omitempty"`
}

// ReviewRequest submits one independent reviewer's decision.
type ReviewRequest struct {
	OperationID task.OperationID    `json:"operation_id"`
	PersonnelID catalog.PersonnelID `json:"personnel_id"`
	Generation  task.Generation     `json:"generation"`
	Decision    arbiter.Verdict     `json:"decision"`
}

// ReviewResult reports how many independent reviews are now present.
type ReviewResult struct {
	Reviews int `json:"reviews"`
}

// FinalizeRequest requests a terminal verdict (or cancellation).
type FinalizeRequest struct {
	OperationID task.OperationID `json:"operation_id"`
	Generation  task.Generation  `json:"generation"`
	Cancelled   bool             `json:"cancelled,omitempty"`
}

// FinalizeResult is the single terminal decision.
type FinalizeResult struct {
	Verdict    arbiter.Verdict `json:"verdict"`
	Credential string          `json:"credential"`
	Reason     string          `json:"reason"`
}

// TaskDetail is the aggregate projection plus evidence closure and reasons.
type TaskDetail struct {
	Task    task.Task `json:"task"`
	Closed  bool      `json:"closed"`
	Reasons []string  `json:"reasons,omitempty"`
}

// AuditEvent is one sortable audit record (failure boundary 4/5).
type AuditEvent struct {
	Seq    int64   `json:"seq"`
	TaskID task.ID `json:"task_id"`
	Clock  int64   `json:"clock"`
	Reason string  `json:"reason"`
}
