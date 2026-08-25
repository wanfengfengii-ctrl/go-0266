// Package ledger implements the basket-sample and resource-lease book.
//
// It owns the one-shot locks over cut-seed batch numbers, basket seals, blind
// codes, healing-shed slots, probe windows, sprout-room time slots and test
// wells (component traceability: 网筐样本与资源占用账簿 -> ledger). A unique
// constraint on (resource type, resource key) guarantees a resource can be held
// by only one open task at a time (domain rule 2).
package ledger

import "potatoeye-cutseed-sprout-gate/task"

// ResourceType enumerates the one-shot lockable resources.
type ResourceType string

const (
	ResourceBatchNumber ResourceType = "batch_number" // 切块批号
	ResourceBasketSeal  ResourceType = "basket_seal"  // 网筐封签
	ResourceBlindCode   ResourceType = "blind_code"   // 盲码
	ResourceHealingShed ResourceType = "healing_shed" // 愈伤棚位
	ResourceProbeWindow ResourceType = "probe_window" // 探头窗口
	ResourceSproutSlot  ResourceType = "sprout_slot"  // 催芽室时隙
	ResourceTestWell    ResourceType = "test_well"    // 检测孔位
)

// Valid reports whether the resource type is a known one-shot resource.
func (rt ResourceType) Valid() bool {
	switch rt {
	case ResourceBatchNumber, ResourceBasketSeal, ResourceBlindCode,
		ResourceHealingShed, ResourceProbeWindow, ResourceSproutSlot, ResourceTestWell:
		return true
	default:
		return false
	}
}

// ResourceKey identifies a concrete resource instance.
type ResourceKey string

// LeaseKey composes the unique (type, key) pair enforced by a DB unique
// constraint during lease acquisition.
func LeaseKey(rt ResourceType, key ResourceKey) string {
	return string(rt) + ":" + string(key)
}

// LeaseState is the lifecycle of a lease.
type LeaseState int

const (
	LeaseOpen     LeaseState = iota // 占用中
	LeaseReleased                   // 已释放
)

// Lease is a one-shot resource occupancy by a task generation.
type Lease struct {
	ResourceType ResourceType
	ResourceKey  ResourceKey
	TaskID       task.ID
	Generation   task.Generation
	State        LeaseState
	StartClock   int64
	EndClock     int64
}

// BasketSample binds a basket seal to a blind code. A blind code is only a
// sample index before reveal; early reveal, duplicate blind codes or a reveal
// that mismatches the sealed sample are all rejected (domain rule 4).
type BasketSample struct {
	TaskID     task.ID
	BasketSeal string
	BlindCode  string
	Sealed     bool
	Revealed   bool
	Generation task.Generation
}

// Ledger is the boundary for resource leases and basket samples.
type Ledger interface {
	// Acquire atomically claims a lease or returns a conflict on collision.
	Acquire(l Lease) error
	// Held returns the owning task and whether the resource is currently held.
	Held(rt ResourceType, key ResourceKey) (task.ID, bool)
}
