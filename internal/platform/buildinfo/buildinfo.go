//intenza:paradigm imperative-shell

// Package buildinfo exposes the identity of the running binary.
//
// Version and Commit can be pinned at link time:
//
//	go build -ldflags "-X github.com/flaviotinococoutinho/intenza/internal/platform/buildinfo.Version=v0.1.0"
//
// Otherwise the VCS stamp embedded by the Go toolchain is used.
package buildinfo

import "runtime/debug"

// Set by -ldflags. Empty values fall back to the embedded VCS stamp.
var (
	Version = "dev"
	Commit  = ""
)

// Info identifies a build.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	GoVersion string `json:"go_version"`
	Dirty     bool   `json:"dirty"`
}

// Read returns the build identity, preferring link-time values.
func Read() Info {
	info := Info{Version: Version, Commit: Commit}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	info.GoVersion = bi.GoVersion
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if info.Commit == "" {
				info.Commit = s.Value
			}
		case "vcs.modified":
			info.Dirty = s.Value == "true"
		}
	}
	return info
}
