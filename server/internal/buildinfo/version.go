// Package buildinfo carries the identity of this build.
//
// Version is kept in step with the repository's VERSION file by
// scripts/sync-version.ts, so that every artifact produced from a commit
// reports the same string. Commit and BuildTime are injected at link time by
// the release build; they are empty in a development build.
package buildinfo

// Version is the release version. Do not edit by hand: change the VERSION file
// at the repository root and run "bun run version:sync".
const Version = "0.1.0"

// ProtocolVersion identifies the federation wire protocol this build speaks,
// newest first where it speaks several (see federation.SupportedVersions). It
// moves independently of Version: the software can be released many times
// without the protocol changing.
const ProtocolVersion = "tdc/1"

// APIVersion is the version of the client API under /api/v1, which /api also
// serves. It changes only when something a client relies on stops working.
const APIVersion = "1"

var (
	// Commit is the git revision this binary was built from.
	Commit = ""
	// BuildTime is the RFC 3339 timestamp of the build.
	BuildTime = ""
)

// Info describes this build for humans and for compatibility checks.
type Info struct {
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocolVersion"`
	APIVersion      string `json:"apiVersion"`
	Commit          string `json:"commit,omitempty"`
	BuildTime       string `json:"buildTime,omitempty"`
}

// Current returns the identity of this build.
func Current() Info {
	return Info{
		Version:         Version,
		ProtocolVersion: ProtocolVersion,
		APIVersion:      APIVersion,
		Commit:          Commit,
		BuildTime:       BuildTime,
	}
}
