package store

const (
	// AssigneeMe is the canonical token used throughout the system when the assignee is the current user.
	// Set by mapFlexToTodo when assignee_id matches the current user's ID.
	AssigneeMe = "me"

	// AssigneeCurrentUser is a sentinel accepted from AI or manual input as an alias for AssigneeMe.
	AssigneeCurrentUser = "__current_user__"
)

// IsSelfAssigneeToken reports whether s is a canonical self-referential assignee token.
func IsSelfAssigneeToken(s string) bool {
	return s == AssigneeMe || s == AssigneeCurrentUser
}
