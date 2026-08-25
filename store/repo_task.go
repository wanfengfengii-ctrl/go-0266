package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/fixed"
	"potatoeye-cutseed-sprout-gate/task"
)

// TaskRow is the flattened persistence shape of a task aggregate.
type TaskRow struct {
	ID                task.ID
	Batch             task.BatchNumber
	State             task.State
	Generation        task.Generation
	Plot              catalog.Plot
	Variety           catalog.VarietyGeneration
	Cellar            catalog.CellarSummary
	Disinfect         catalog.DisinfectBatch
	SuberizationGrade int
	WaterLossRate     int64
	DiseaseCt         int64
	Reviewers         []catalog.PersonnelID
	ObserveDays       []int
	Points            []string
	HealingShed       string
	ProbeWindow       string
	SproutSlot        string
	TestWells         []string
	CreatedClock      int64
	FinalReason       string
}

const taskColumns = `id, batch, state, generation, plot, variety, cellar, disinfect,
	suberization_grade, water_loss_rate, disease_ct, reviewers, observe_days, points,
	healing_shed, probe_window, sprout_slot, test_wells, created_clock, final_reason`

func scanTaskRow(scan func(...any) error) (TaskRow, error) {
	var r TaskRow
	var reviewers, days, points, wells string
	if err := scan(&r.ID, &r.Batch, &r.State, &r.Generation, &r.Plot, &r.Variety,
		&r.Cellar, &r.Disinfect, &r.SuberizationGrade, &r.WaterLossRate, &r.DiseaseCt,
		&reviewers, &days, &points, &r.HealingShed, &r.ProbeWindow, &r.SproutSlot, &wells,
		&r.CreatedClock, &r.FinalReason); err != nil {
		return r, err
	}
	r.Reviewers = splitPersonnel(reviewers)
	r.ObserveDays = splitInts(days)
	r.Points = splitStrings(points)
	r.TestWells = splitStrings(wells)
	return r, nil
}

func (r TaskRow) snapshot() task.LockSnapshot {
	return task.LockSnapshot{
		Plot:           r.Plot,
		Variety:        r.Variety,
		CellarSummary:  r.Cellar,
		DisinfectBatch: r.Disinfect,
		Thresholds: catalog.Thresholds{
			SuberizationGrade: r.SuberizationGrade,
			WaterLossRate:     fixed.New(r.WaterLossRate),
			DiseaseCt:         fixed.New(r.DiseaseCt),
		},
		Reviewers: r.Reviewers,
	}
}

func (r TaskRow) toTask() task.Task {
	return task.Task{
		ID:          r.ID,
		Batch:       r.Batch,
		State:       r.State,
		Generation:  r.Generation,
		Snapshot:    r.snapshot(),
		FinalReason: r.FinalReason,
	}
}

// getTaskRow loads a task row within the given transaction or connection.
func getTaskRow(ctx context.Context, q dbtx, id task.ID) (TaskRow, error) {
	row := q.QueryRowContext(ctx, "SELECT "+taskColumns+" FROM tasks WHERE id = ?", id)
	r, err := scanTaskRow(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// Current implements task.Aggregate for the store (used by GET and internally).
func (s *SQLite) Current(id task.ID) (task.Task, error) {
	r, err := getTaskRow(context.Background(), s.db, id)
	if err != nil {
		return task.Task{}, err
	}
	return r.toTask(), nil
}

func insertTask(ctx context.Context, tx *sql.Tx, r TaskRow) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO tasks ("+taskColumns+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		r.ID, r.Batch, r.State, r.Generation, r.Plot, r.Variety, r.Cellar, r.Disinfect,
		r.SuberizationGrade, r.WaterLossRate, r.DiseaseCt,
		joinPersonnel(r.Reviewers), joinInts(r.ObserveDays), joinStrings(r.Points),
		r.HealingShed, r.ProbeWindow, r.SproutSlot, joinStrings(r.TestWells),
		r.CreatedClock, r.FinalReason)
	return err
}

func updateTaskState(ctx context.Context, tx *sql.Tx, id task.ID, state task.State, generation task.Generation, finalReason string) error {
	_, err := tx.ExecContext(ctx,
		"UPDATE tasks SET state = ?, generation = ?, final_reason = ? WHERE id = ?",
		state, generation, finalReason, id)
	return err
}

func splitPersonnel(s string) []catalog.PersonnelID {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]catalog.PersonnelID, len(parts))
	for i, p := range parts {
		out[i] = catalog.PersonnelID(p)
	}
	return out
}

func joinPersonnel(v []catalog.PersonnelID) string {
	parts := make([]string, len(v))
	for i, p := range v {
		parts[i] = string(p)
	}
	return strings.Join(parts, ",")
}

func splitStrings(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func joinStrings(v []string) string { return strings.Join(v, ",") }
