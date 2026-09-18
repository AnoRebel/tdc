//go:build !gtk3

package updater

/*
platformVariant names the toolkit this binary was built against, where that
changes which artifact it can run.

Empty is the ordinary case: the build has no variant and asks the feed for its
bare platform. See variant_gtk3.go for the one build that does.
*/
const platformVariant = ""
