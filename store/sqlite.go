package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"sync"

	_ "modernc.org/sqlite"

	"potatoeye-cutseed-sprout-gate/arbiter"
	"potatoeye-cutseed-sprout-gate/catalog"
	"potatoeye-cutseed-sprout-gate/evidence"
	"potatoeye-cutseed-sprout-gate/fixed"
	"potatoeye-cutseed-sprout-gate/ledger"
	"potatoeye-cutseed-sprout-gate/task"
)

// SQLite is the production Store: a single-node SQLite WAL database that
// persists the task aggregate, resource leases, coverage matrix, evidence
// version chain, idempotency records, adapter calls, reviews, final decisions
// and audit events so the process recovers deterministically after restart.
type SQLite struct {
	db       *sql.DB
	registry *catalog.Registry

	faultMu sync.Mutex
	faults  map[string]error
}

// Open opens (creating if necessary) the SQLite database at path in WAL mode
// and migrates the schema. It seeds a default rule directory on first use.
func Open(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := configure(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &SQLite{db: db, faults: make(map[string]error)}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.seedDefaults(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s.registry, err = s.loadRegistry()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// OpenMemory opens an isolated in-memory database for tests. It shares the same
// schema and seeding as the file-backed store.
func OpenMemory() (*SQLite, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := configure(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &SQLite{db: db, faults: make(map[string]error)}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.seedDefaults(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s.registry, err = s.loadRegistry()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func configure(db *sql.DB) error {
	db.SetMaxOpenConns(4)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return err
		}
	}
	return nil
}

// Close releases the underlying database.
func (s *SQLite) Close() error { return s.db.Close() }

// Catalog returns the seeded in-memory rule directory.
func (s *SQLite) Catalog() catalog.Catalog { return s.registry }

// Tasks returns the task aggregate read boundary.
func (s *SQLite) Tasks() task.Aggregate { return s }

// Ledger returns the ledger lease boundary.
func (s *SQLite) Ledger() ledger.Ledger { return s }

// Evidence returns the evidence append boundary.
func (s *SQLite) Evidence() evidence.Evidence { return s }

// Arbiter returns the final arbitration boundary.
func (s *SQLite) Arbiter() arbiter.Arbiter { return s }

// InjectFault registers an adapter fault for a (adapter, target) key so tests
// can deterministically drive rejected/disconnected/timeout/malformed readings.
func (s *SQLite) InjectFault(key string, err error) {
	s.faultMu.Lock()
	defer s.faultMu.Unlock()
	s.faults[key] = err
}

// ClearFault removes a previously injected adapter fault.
func (s *SQLite) ClearFault(key string) {
	s.faultMu.Lock()
	defer s.faultMu.Unlock()
	delete(s.faults, key)
}

func (s *SQLite) faultFor(key string) error {
	s.faultMu.Lock()
	defer s.faultMu.Unlock()
	return s.faults[key]
}

// nextClock returns the next monotonically increasing logical clock value,
// persisted in the clock_seq table so it survives restart.
func (s *SQLite) nextClock(ctx context.Context, tx *sql.Tx) (int64, error) {
	res, err := tx.ExecContext(ctx, "INSERT INTO clock_seq DEFAULT VALUES")
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

const schema = `
CREATE TABLE IF NOT EXISTS catalog_rules (
  version INTEGER NOT NULL,
  plot TEXT NOT NULL,
  variety TEXT NOT NULL,
  cellar TEXT NOT NULL,
  disinfect TEXT NOT NULL,
  suberization_grade INTEGER NOT NULL,
  water_loss_rate INTEGER NOT NULL,
  disease_ct INTEGER NOT NULL,
  observe_days TEXT NOT NULL,
  PRIMARY KEY (version, plot, variety)
);
CREATE TABLE IF NOT EXISTS personnel (
  personnel_id TEXT NOT NULL,
  role TEXT NOT NULL,
  version INTEGER NOT NULL,
  PRIMARY KEY (personnel_id, role, version)
);
CREATE TABLE IF NOT EXISTS clock_seq (
  seq INTEGER PRIMARY KEY AUTOINCREMENT
);
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  batch TEXT NOT NULL,
  state TEXT NOT NULL,
  generation INTEGER NOT NULL,
  plot TEXT NOT NULL,
  variety TEXT NOT NULL,
  cellar TEXT NOT NULL,
  disinfect TEXT NOT NULL,
  suberization_grade INTEGER NOT NULL,
  water_loss_rate INTEGER NOT NULL,
  disease_ct INTEGER NOT NULL,
  reviewers TEXT NOT NULL,
  observe_days TEXT NOT NULL,
  points TEXT NOT NULL DEFAULT '',
  healing_shed TEXT NOT NULL DEFAULT '',
  probe_window TEXT NOT NULL DEFAULT '',
  sprout_slot TEXT NOT NULL DEFAULT '',
  test_wells TEXT NOT NULL DEFAULT '',
  created_clock INTEGER NOT NULL,
  final_reason TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS cut_confirmations (
  task_id TEXT NOT NULL,
  personnel_id TEXT NOT NULL,
  PRIMARY KEY (task_id, personnel_id)
);
CREATE TABLE IF NOT EXISTS basket_samples (
  task_id TEXT NOT NULL,
  basket_seal TEXT NOT NULL,
  blind_code TEXT NOT NULL,
  sealed INTEGER NOT NULL,
  revealed INTEGER NOT NULL,
  generation INTEGER NOT NULL,
  PRIMARY KEY (task_id, basket_seal)
);
CREATE UNIQUE INDEX IF NOT EXISTS basket_blind ON basket_samples(blind_code);
CREATE TABLE IF NOT EXISTS resource_leases (
  resource_type TEXT NOT NULL,
  resource_key TEXT NOT NULL,
  task_id TEXT NOT NULL,
  generation INTEGER NOT NULL,
  state INTEGER NOT NULL,
  start_clock INTEGER NOT NULL,
  end_clock INTEGER NOT NULL,
  PRIMARY KEY (resource_type, resource_key)
);
CREATE TABLE IF NOT EXISTS observations (
  task_id TEXT NOT NULL,
  age_day INTEGER NOT NULL,
  basket_seal TEXT NOT NULL,
  point_id TEXT NOT NULL,
  suberization_grade INTEGER NOT NULL,
  water_loss_blocks INTEGER NOT NULL,
  rot_blocks INTEGER NOT NULL,
  suberized_blocks INTEGER NOT NULL,
  total_blocks INTEGER NOT NULL,
  bud_eye_active INTEGER NOT NULL,
  bud_eye_capacity INTEGER NOT NULL,
  remedial INTEGER NOT NULL,
  version INTEGER NOT NULL,
  PRIMARY KEY (task_id, age_day, basket_seal, point_id)
);
CREATE TABLE IF NOT EXISTS disease_evidence (
  task_id TEXT NOT NULL,
  blind_code TEXT NOT NULL,
  well_id TEXT NOT NULL,
  lesion_diameter INTEGER NOT NULL,
  exudate_level INTEGER NOT NULL,
  ct_value INTEGER NOT NULL,
  rejudge_gen INTEGER NOT NULL,
  source_call_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  PRIMARY KEY (task_id, blind_code, well_id, version)
);
CREATE TABLE IF NOT EXISTS idempotency_records (
  operation_id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  request_hash TEXT NOT NULL,
  response_hash TEXT NOT NULL,
  response_body TEXT NOT NULL DEFAULT '',
  generation INTEGER NOT NULL,
  created_clock INTEGER NOT NULL,
  conflict INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS adapter_calls (
  call_id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL,
  adapter TEXT NOT NULL,
  target TEXT NOT NULL,
  status TEXT NOT NULL,
  retry_count INTEGER NOT NULL,
  clock INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS reviews (
  task_id TEXT NOT NULL,
  personnel_id TEXT NOT NULL,
  decision TEXT NOT NULL,
  generation INTEGER NOT NULL,
  PRIMARY KEY (task_id, personnel_id)
);
CREATE TABLE IF NOT EXISTS final_decisions (
  task_id TEXT PRIMARY KEY,
  verdict TEXT NOT NULL,
  generation INTEGER NOT NULL,
  credential TEXT NOT NULL,
  reason TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS audit_events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT NOT NULL,
  clock INTEGER NOT NULL,
  reason TEXT NOT NULL
);
`

func (s *SQLite) migrate() error {
	for _, stmt := range strings.Split(schema, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	// Additive migrations for databases created before a column existed. Each is
	// idempotent: SQLite errors when the column is already present, which we
	// treat as a no-op. Keeping the stored response body lets an idempotent
	// replay return the exact original result instead of a re-derived one.
	additive := []string{
		"ALTER TABLE idempotency_records ADD COLUMN response_body TEXT NOT NULL DEFAULT ''",
	}
	for _, stmt := range additive {
		if _, err := s.db.Exec(stmt); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return err
			}
		}
	}
	return nil
}

func (s *SQLite) seedDefaults() error {
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM catalog_rules").Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	// Seed the two documented rules and four personnel so lock requests can
	// validate against a real directory on first use.
	rules := []catalog.Rule{
		{
			Version: 1, Plot: "P-01", Variety: "V-G2", Cellar: "C-01", Disinfect: "D-01",
			Thresholds: catalog.Thresholds{
				SuberizationGrade: 2, WaterLossRate: fixed.New(5000), DiseaseCt: fixed.New(35000),
			},
			ObserveDays: []int{1, 2, 3},
		},
		{
			Version: 1, Plot: "P-02", Variety: "V-G3", Cellar: "C-02", Disinfect: "D-02",
			Thresholds: catalog.Thresholds{
				SuberizationGrade: 3, WaterLossRate: fixed.New(4000), DiseaseCt: fixed.New(34000),
			},
			ObserveDays: []int{1, 2},
		},
	}
	for _, r := range rules {
		if _, err := s.db.Exec(
			"INSERT INTO catalog_rules (version, plot, variety, cellar, disinfect, suberization_grade, water_loss_rate, disease_ct, observe_days) VALUES (?,?,?,?,?,?,?,?,?)",
			r.Version, r.Plot, r.Variety, r.Cellar, r.Disinfect,
			r.Thresholds.SuberizationGrade, r.Thresholds.WaterLossRate.Raw(), r.Thresholds.DiseaseCt.Raw(),
			joinInts(r.ObserveDays),
		); err != nil {
			return err
		}
	}
	for _, p := range []struct {
		id   catalog.PersonnelID
		role catalog.Role
	}{{"cutter-1", catalog.RoleCutter}, {"cutter-2", catalog.RoleCutter},
		{"reviewer-1", catalog.RoleReviewer}, {"reviewer-2", catalog.RoleReviewer}} {
		if _, err := s.db.Exec(
			"INSERT INTO personnel (personnel_id, role, version) VALUES (?,?,?)",
			p.id, p.role, 1,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLite) loadRegistry() (*catalog.Registry, error) {
	reg := catalog.NewRegistry()
	rows, err := s.db.Query("SELECT version, plot, variety, cellar, disinfect, suberization_grade, water_loss_rate, disease_ct, observe_days FROM catalog_rules")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var r catalog.Rule
		var days string
		if err := rows.Scan(&r.Version, &r.Plot, &r.Variety, &r.Cellar, &r.Disinfect,
			&r.Thresholds.SuberizationGrade, &r.Thresholds.WaterLossRate, &r.Thresholds.DiseaseCt, &days); err != nil {
			return nil, err
		}
		r.ObserveDays = splitInts(days)
		reg.AddRule(r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	prows, err := s.db.Query("SELECT personnel_id, role, version FROM personnel")
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	for prows.Next() {
		var id catalog.PersonnelID
		var role catalog.Role
		var version catalog.RuleVersion
		if err := prows.Scan(&id, &role, &version); err != nil {
			return nil, err
		}
		reg.AddPersonnel(id, role, version)
	}
	return reg, prows.Err()
}

func joinInts(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func splitInts(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]int, len(parts))
	for i, p := range parts {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}
