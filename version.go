package main

import "runtime/debug"

// version is reported by -version. Override at link time:
//
//	go build -ldflags "-X main.version=v1.2.3"
//
// go install of a tagged module uses the module version when this is still "dev".
var version = "dev"

func currentVersion() string {
	if version != "" && version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}
