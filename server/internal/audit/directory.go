package audit

/*
What the directory records.

Only exercises of authority over somebody else's profile, and the attestations
a moderator makes. Somebody editing their own profile is ordinary use of the
application, and recording every save would bury the entries anybody reviewing
the log is looking for — the same reasoning that keeps a person's own message
deletions out of it.
*/
const (
	/*
		ActionDirectoryProfileModerated records a moderator changing what
		somebody's profile says, or how visible it is.

		The detail names which fields changed and why, never their content: the
		log is not a second store of what somebody wrote about themselves.
	*/
	ActionDirectoryProfileModerated Action = "directory.profile.moderated"

	/*
		ActionDirectoryProfileViewed records a moderator reading a profile as
		its owner sees it — including whatever the owner has hidden.

		Moderation needs the access, so it is not refused; it is accounted for,
		which is what makes the exception reviewable by the person it was used
		on. The same bargain as ActionWithheldDetailsViewed.
	*/
	ActionDirectoryProfileViewed Action = "directory.profile.viewed"

	/*
		Decisions about the skill vocabulary.

		Recorded because the vocabulary is what search is reliable against: a
		skill that vanished, or two that became one, changes who is found for
		what, and somebody asking why should find who decided and when.
	*/
	ActionDirectorySkillAdded    Action = "directory.skill.added"
	ActionDirectorySkillApproved Action = "directory.skill.approved"
	ActionDirectorySkillRejected Action = "directory.skill.rejected"
	ActionDirectorySkillMerged   Action = "directory.skill.merged"

	/*
		The life of a verification: granted with what was checked, revoked
		with why, and cleared when the claim it covered was edited.

		Cleared is attributed to the owner whose edit caused it. Without it a
		reader would see a verification granted and never withdrawn, and could
		not tell why the profile stopped showing it.
	*/
	ActionDirectoryVerificationGranted Action = "directory.verification.granted"
	ActionDirectoryVerificationRevoked Action = "directory.verification.revoked"
	ActionDirectoryVerificationCleared Action = "directory.verification.cleared"
)
