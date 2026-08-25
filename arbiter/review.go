package arbiter

import (
	"errors"
	"fmt"
)

// Review validation errors enforcing role separation and reviewer uniqueness
// (acceptance 3: 复核人员与切块确认人员角色重合拒绝).
var (
	// ErrRoleOverlap reports a reviewer who also acted as a cutter on the task.
	ErrRoleOverlap = errors.New("arbiter: reviewer role overlap")
	// ErrDuplicateReviewer reports two reviews from the same reviewer.
	ErrDuplicateReviewer = errors.New("arbiter: duplicate reviewer")
)

// ValidateReview enforces role separation and reviewer uniqueness for a single
// independent review. cutters are the personnel who performed cut confirmations
// on the same task; existing holds the reviewers who already submitted.
func ValidateReview(review Review, existing []Review, cutters []string) error {
	for _, c := range cutters {
		if review.PersonnelID == c {
			return fmt.Errorf("%w: %s", ErrRoleOverlap, review.PersonnelID)
		}
	}
	for _, r := range existing {
		if r.PersonnelID == review.PersonnelID {
			return fmt.Errorf("%w: %s", ErrDuplicateReviewer, review.PersonnelID)
		}
	}
	return nil
}
