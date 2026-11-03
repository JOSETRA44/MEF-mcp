// Package version holds the single source of truth for the release version.
//
// server.json, the npm wrapper and the describe manifest all read this value;
// a test asserts they agree so a release cannot ship with drifting numbers.
package version

import (
	"runtime/debug"
	"strings"
)

// Version is bumped by hand on release. `go install ...@vX.Y.Z` builds report
// the module version instead when it is available (see Get).
const Version = "0.2.0"

// Name is the manifest/tool name used across the arsenal.
const Name = "mef"

// Get returns the module version stamped by `go install pkg@vX.Y.Z`, falling
// back to the constant otherwise.
//
// Go also stamps a pseudo-version ("v0.0.0-20260911203120-40de1128+dirty")
// when installing from a checkout with no release tag. Reporting that would
// tell a user they run 0.0.0, so pseudo-versions are ignored in favour of the
// constant, which is what server.json and the npm wrapper agree with.
func Get() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		v := info.Main.Version
		if v != "" && v != "(devel)" && v[0] == 'v' &&
			!strings.HasPrefix(v, "v0.0.0-") && !strings.Contains(v, "+dirty") {
			return v[1:]
		}
	}
	return Version
}
