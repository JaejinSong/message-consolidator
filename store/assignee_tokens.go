package store

const (
	// AssigneeMe is kept for backward-compat: recognizing AI-output "me" tokens and legacy DB records.
	// Not used as a stored value — applyAssigneeRules normalizes to the user's canonical name.
	AssigneeMe = "me"

	// AssigneeCurrentUser is an AI-output sentinel normalized to the user's canonical name on ingestion.
	AssigneeCurrentUser = "__current_user__"
)

// IsSelfAssigneeToken reports whether s is a self-referential AI-output or legacy token.
func IsSelfAssigneeToken(s string) bool {
	return s == AssigneeMe || s == AssigneeCurrentUser
}
