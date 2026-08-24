package catalog

import "fmt"

// Registry is a concrete in-memory Catalog implementation. It holds the
// plot/variety rules and the qualified-personnel directory that the lock step
// validates against. The store seeds a Registry from the catalog_rules table at
// startup so that every lock request is checked against a real rule version.
type Registry struct {
	rules     map[string]Rule
	personnel map[string]Role
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		rules:     make(map[string]Rule),
		personnel: make(map[string]Role),
	}
}

// AddRule records a rule under its plot/variety key.
func (r *Registry) AddRule(rule Rule) {
	r.rules[ruleKey(rule.Plot, rule.Variety)] = rule
}

// AddPersonnel records a personnel member with a role and rule version.
func (r *Registry) AddPersonnel(p PersonnelID, role Role, version RuleVersion) {
	r.personnel[personnelKey(p, role, version)] = role
}

// Match returns the rule for a plot/variety pair, or ErrMismatch when no rule
// binds that pair (domain rule 3).
func (r *Registry) Match(plot Plot, variety VarietyGeneration) (Rule, error) {
	rule, ok := r.rules[ruleKey(plot, variety)]
	if !ok {
		return Rule{}, fmt.Errorf("%w: plot %q variety %q", ErrMismatch, plot, variety)
	}
	return rule, nil
}

// Qualified reports whether p holds role under the given rule version.
func (r *Registry) Qualified(p PersonnelID, role Role, version RuleVersion) bool {
	_, ok := r.personnel[personnelKey(p, role, version)]
	return ok
}

func ruleKey(plot Plot, variety VarietyGeneration) string {
	return string(plot) + "\x00" + string(variety)
}

func personnelKey(p PersonnelID, role Role, version RuleVersion) string {
	return fmt.Sprintf("%s\x00%s\x00%d", p, role, version)
}
