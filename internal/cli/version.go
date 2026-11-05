package cli

import "runtime/debug"

// Version is reported by -version and the admin UI. main sets it from its
// link-time value (-ldflags "-X main.version=v1.2.3"). When it is still "dev",
// the module version from go install is used instead.
var Version = "dev"

func currentVersion() string {
	if Version != "" && Version != "dev" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return Version
}
