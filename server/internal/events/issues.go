package events

/*
What a project's stream records.

A project's history is a room's log like any other — signed by this server,
numbered in one total order, verifiable from the row alone — so issues need
nothing from this package but names for what happened to them. The envelope,
the signing and the ordering are exactly those of a message, which is the
reason for putting them here rather than in a log of their own.

Every issue event carries the issue's identifier as content.issue, which is
how one issue's history is read out of its project's stream. A change to a
field carries field, previous and new, so a reader can show what changed
without knowing every type in advance.
*/
const (
	// The stream's own beginning, and changes to the project itself.
	TypeProjectCreated       Type = "project.created"
	TypeProjectUpdated       Type = "project.updated"
	TypeProjectMemberAdded   Type = "project.member_added"
	TypeProjectMemberRemoved Type = "project.member_removed"
	TypeProjectStateAdded    Type = "project.state_added"
	TypeProjectStateChanged  Type = "project.state_changed"
	TypeProjectStateRemoved  Type = "project.state_removed"

	TypeIssueCreated            Type = "issue.created"
	TypeIssueStateChanged       Type = "issue.state_changed"
	TypeIssueTitleChanged       Type = "issue.title_changed"
	TypeIssueDescriptionChanged Type = "issue.description_changed"
	TypeIssueDeleted            Type = "issue.deleted"
	TypeIssueAssigned           Type = "issue.assigned"
	TypeIssueUnassigned         Type = "issue.unassigned"
	TypeIssueLabeled            Type = "issue.labeled"
	TypeIssueUnlabeled          Type = "issue.unlabeled"
	TypeIssuePriorityChanged    Type = "issue.priority_changed"
	TypeIssueRelated            Type = "issue.related"
	TypeIssueUnrelated          Type = "issue.unrelated"
	TypeIssueMilestoneChanged   Type = "issue.milestone_changed"
	TypeIssueCycleChanged       Type = "issue.cycle_changed"
)
