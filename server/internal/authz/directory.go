package authz

/*
The directory's permissions.

Declared here rather than in the catalogue literal so the directory can be
added, and one day removed, without editing the list every other feature
shares. Registration refuses a name already taken, for the same reason route
and setting registration do: two features claiming one permission is a conflict
worth stopping the build over rather than letting one silently win.

None of these is administrative. They govern what people have written about
themselves, not the server, and the administrator floor has no business
protecting them.
*/
const (
	// PermDirectoryProfileManage is changing somebody else's directory profile
	// — in practice, clearing something abusive from it — and seeing a profile
	// its owner has hidden, which moderating it requires.
	PermDirectoryProfileManage Permission = "directory.profile.manage"

	/*
		PermDirectorySkillsManage is deciding what the skill vocabulary
		contains: promoting a proposed skill so it can be searched for, or
		rejecting it.

		Its own permission rather than folded into moderation.review. Reviewing
		reports is judging conduct; curating a vocabulary is judging what
		counts as the same skill, and a community may well want the second done
		by somebody it would not ask to do the first.
	*/
	PermDirectorySkillsManage Permission = "directory.skills.manage"

	/*
		PermDirectoryVerify is attesting that one specific claim on a profile
		checks out, and withdrawing that attestation.

		Not self-service and not automatic: a claim somebody verifies for
		themselves is simply the self-asserted claim, which the directory
		already shows as such.
	*/
	PermDirectoryVerify Permission = "directory.verify"
)

func init() {
	define(map[Permission]Definition{
		PermDirectoryProfileManage: {
			Name:        PermDirectoryProfileManage,
			Description: "Change or clear another person's directory profile, and see one they have hidden",
			Scopes:      []Scope{ScopeServer},
		},
		PermDirectorySkillsManage: {
			Name:        PermDirectorySkillsManage,
			Description: "Promote or reject skills proposed for the directory's vocabulary",
			Scopes:      []Scope{ScopeServer},
		},
		PermDirectoryVerify: {
			Name:        PermDirectoryVerify,
			Description: "Verify a specific claim on a directory profile, and revoke a verification",
			Scopes:      []Scope{ScopeServer},
		},
	})
}

// define adds permissions to the catalogue from a feature's own file.
func define(additions map[Permission]Definition) {
	for name, definition := range additions {
		if _, taken := catalogue[name]; taken {
			panic("permission defined twice: " + string(name))
		}
		catalogue[name] = definition
	}
}
