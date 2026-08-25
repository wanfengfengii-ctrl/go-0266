package store

import (
	"context"
	"database/sql"
	"fmt"

	"potatoeye-cutseed-sprout-gate/evidence"
	"potatoeye-cutseed-sprout-gate/task"
)

func appendObservationTx(ctx context.Context, tx *sql.Tx, o evidence.Observation) error {
	remedial := 0
	if o.Remedial {
		remedial = 1
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO observations
		 (task_id, age_day, basket_seal, point_id, suberization_grade, water_loss_blocks,
		  rot_blocks, suberized_blocks, total_blocks, bud_eye_active, bud_eye_capacity, remedial, version)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.TaskID, o.AgeDay, o.BasketSeal, o.PointID, o.SuberizationGrade, o.WaterLossBlocks,
		o.RotBlocks, o.SuberizedBlocks, o.TotalBlocks, o.BudEyeActive, 0, remedial, o.Version)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: age %d seal %q point %q", evidence.ErrDuplicateCoverage, o.AgeDay, o.BasketSeal, o.PointID)
	}
	return err
}

// updateBudEyeTx records a bud-eye active count and its capacity across every
// age-day cell for a (seal, point) pair, marking those cells bud-eye verified.
func updateBudEyeTx(ctx context.Context, tx *sql.Tx, id task.ID, seal evidence.BasketSeal, point evidence.PointID, active, capacity int) error {
	res, err := tx.ExecContext(ctx,
		"UPDATE observations SET bud_eye_active = ?, bud_eye_capacity = ? WHERE task_id = ? AND basket_seal = ?",
		active, capacity, id, seal)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("evidence: no coverage cell for seal %q point %q", seal, point)
	}
	return nil
}

func appendDiseaseTx(ctx context.Context, tx *sql.Tx, d evidence.DiseaseEvidence) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO disease_evidence
		 (task_id, blind_code, well_id, lesion_diameter, exudate_level, ct_value, rejudge_gen, source_call_id, version)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		d.TaskID, d.BlindCode, d.WellID, d.LesionDiameter.Raw(), d.ExudateLevel, d.CtValue.Raw(),
		d.RejudgeGen, d.SourceCallID, d.Version)
	if isUniqueViolation(err) {
		return fmt.Errorf("%w: blind %q well %q version %d", evidence.ErrImmutableVersion, d.BlindCode, d.WellID, d.Version)
	}
	return err
}

func loadObservations(ctx context.Context, q dbtx, id task.ID) ([]evidence.Observation, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT task_id, age_day, basket_seal, point_id, suberization_grade, water_loss_blocks,
		 rot_blocks, suberized_blocks, total_blocks, bud_eye_active, bud_eye_capacity, remedial, version
		 FROM observations WHERE task_id = ? ORDER BY age_day, basket_seal, point_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []evidence.Observation
	for rows.Next() {
		var o evidence.Observation
		var remedial, capacity int
		if err := rows.Scan(&o.TaskID, &o.AgeDay, &o.BasketSeal, &o.PointID, &o.SuberizationGrade,
			&o.WaterLossBlocks, &o.RotBlocks, &o.SuberizedBlocks, &o.TotalBlocks, &o.BudEyeActive,
			&capacity, &remedial, &o.Version); err != nil {
			return nil, err
		}
		o.Remedial = remedial == 1
		out = append(out, o)
	}
	return out, rows.Err()
}

func loadDisease(ctx context.Context, q dbtx, id task.ID) ([]evidence.DiseaseEvidence, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT task_id, blind_code, well_id, lesion_diameter, exudate_level, ct_value, rejudge_gen, source_call_id, version
		 FROM disease_evidence WHERE task_id = ? ORDER BY blind_code, well_id, version`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []evidence.DiseaseEvidence
	for rows.Next() {
		var d evidence.DiseaseEvidence
		if err := rows.Scan(&d.TaskID, &d.BlindCode, &d.WellID, &d.LesionDiameter, &d.ExudateLevel,
			&d.CtValue, &d.RejudgeGen, &d.SourceCallID, &d.Version); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AppendObservation implements evidence.Evidence as a standalone append.
func (s *SQLite) AppendObservation(o evidence.Observation) error {
	return s.withTx(context.Background(), func(tx *sql.Tx) error {
		v, err := nextObservationVersion(context.Background(), tx, o.TaskID)
		if err != nil {
			return err
		}
		o.Version = v
		return appendObservationTx(context.Background(), tx, o)
	})
}

// AppendDisease implements evidence.Evidence as a standalone append.
func (s *SQLite) AppendDisease(d evidence.DiseaseEvidence) error {
	return s.withTx(context.Background(), func(tx *sql.Tx) error {
		v, err := nextDiseaseVersion(context.Background(), tx, d.TaskID)
		if err != nil {
			return err
		}
		d.Version = v
		return appendDiseaseTx(context.Background(), tx, d)
	})
}

func nextObservationVersion(ctx context.Context, q dbtx, id task.ID) (evidence.Version, error) {
	var v int
	err := q.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0)+1 FROM observations WHERE task_id = ?", id).Scan(&v)
	return evidence.Version(v), err
}

func nextDiseaseVersion(ctx context.Context, q dbtx, id task.ID) (evidence.Version, error) {
	var v int
	err := q.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0)+1 FROM disease_evidence WHERE task_id = ?", id).Scan(&v)
	return evidence.Version(v), err
}
