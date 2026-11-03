package updater

import "runtime"

/*
PlatformKey is how this build names itself among a release's artifacts.

Ordinarily the Go platform, as "linux/amd64". A build against a different
toolkit appends its variant, so it is offered its own artifact and the ordinary
build is offered the ordinary one. The two are not interchangeable — that is
the whole reason the variant exists — so a missing variant artifact means there
is nothing this machine can run, not that it should take the other one.
*/
func PlatformKey() string {
	key := runtime.GOOS + "/" + runtime.GOARCH
	if platformVariant != "" {
		key += "-" + platformVariant
	}
	return key
}

// executableSuffix is what the platform expects an executable to be called.
func executableSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
