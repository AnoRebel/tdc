package settings

/*
Changing a username.

A username is half of somebody's address, so changing one is allowed but
paced, and the name given up is kept for its owner for a while. Display names
need no setting: they are free to change, because nothing else is built on
them.
*/
const (
	/*
		UsernameChanges says whether people may change their own username.

		On by default. A name chosen at sixteen should not be a life sentence,
		and the protections below are what make allowing it safe; an operator
		whose community relies on names never changing can turn it off.
	*/
	UsernameChanges Key = "accounts.username_changes"

	/*
		UsernameChangeDays is how long somebody waits between changes.

		Thirty by default: often enough for somebody who chose badly to fix it,
		seldom enough that a name cannot be rotated to dodge a block or to
		confuse people about who is who. Zero means no wait.
	*/
	UsernameChangeDays Key = "accounts.username_change_days"

	/*
		UsernameHoldDays is how long a name somebody gave up stays theirs.

		Ninety by default. Within it nobody else may register or take the name,
		so a stranger cannot pick it up and receive what was meant for the
		person who had it. After it the name is free; until somebody does take
		it, old links still find the person who had it. Zero releases a name
		at once.
	*/
	UsernameHoldDays Key = "accounts.username_hold_days"
)

func init() {
	define(map[Key]definition{
		UsernameChanges: {
			fallback:    "true",
			description: "Whether people may change their own username.",
			validate:    boolean,
		},
		UsernameChangeDays: {
			fallback:    "30",
			description: "Days somebody waits between username changes. 0 means no wait.",
			validate:    nonNegative,
		},
		UsernameHoldDays: {
			fallback:    "90",
			description: "Days a username somebody gave up is kept for them before anybody else may take it. 0 releases it at once.",
			validate:    nonNegative,
		},
	})
}
