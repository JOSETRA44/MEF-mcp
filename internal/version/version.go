// Package version holds the single source of truth for the release version.
//
// server.json, the npm wrapper and the describe manifest all read this value;
// a test asserts they agree so a release cannot ship with drifting numbers.
package version

import "runtime/debug"

// Version is bumped by hand on release. `go install ...@vX.Y.Z` builds report
// the module version instead when it is available (see Get).
const Version = "0.1.0"

// Name is the manifest/tool name used across the arsenal.
const Name = "mef"

// Get returns the module version stamped by `go install pkg@version`, falling
// back to the constant for source builds, where Go reports "(devel)".
func Get() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		v := info.Main.Version
		if v != "" && v != "(devel)" && v[0] == 'v' {
			return v[1:]
		}
	}
	return Version
}
