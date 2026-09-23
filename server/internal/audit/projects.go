package audit

/*
What is recorded about projects.

Only the acts that decide who can see what. An issue's own history is its
project's event stream, and copying it here would make a second record of the
same changes that could come to disagree with the first; what belongs in the
audit log is the administrative layer above that — a project started, opened
to everybody or closed to all but its members, somebody let in or shown out.
*/
const (
	ActionProjectCreated           Action = "project.created"
	ActionProjectVisibilityChanged Action = "project.visibility.changed"
	ActionProjectMemberAdded       Action = "project.member.added"
	ActionProjectMemberRemoved     Action = "project.member.removed"
	ActionProjectPermissionChanged Action = "project.permission.changed"
)
