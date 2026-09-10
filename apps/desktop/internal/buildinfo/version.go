// Package buildinfo carries the identity of this build.
//
// Version is kept in step with the repository's VERSION file by
// scripts/sync-version.ts, the same way the server's is. Two places claiming a
// version is one place being wrong, and this is the one people read on an
// About screen.
package buildinfo

// Version is the release version. Do not edit by hand: change the VERSION file
// at the repository root and run `bun run scripts/sync-version.ts`.
const Version = "0.1.0"
