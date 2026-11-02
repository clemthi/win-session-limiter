// Package version holds the program's version number. Release builds set it
// from the release tag with
//
//	-ldflags "-X winsessionlimiter/internal/version.Version=1.2.3"
//
// (see .github/workflows/release.yml); local builds report "dev".
package version

var Version = "dev"
