// Package buildinfo reports the build this binary runs: its stamped version,
// commit and build time, and the recent merge history embedded at build time.
package buildinfo

import (
	"embed"
	"io/fs"

	protocol "github.com/mhersson/contextmatrix-protocol"
	"github.com/mhersson/contextmatrix-protocol/changelog"
)

//go:embed all:history
var history embed.FS

// -ldflags -X stamps these; a bare go build leaves them empty.
var (
	version   string
	commit    string
	buildTime string
)

// Info returns the build this binary was made from.
func Info() protocol.BuildInfo {
	return infoFrom(history)
}

func infoFrom(fsys fs.FS) protocol.BuildInfo {
	// A read error means no history; Parse(nil, ...) returns an empty slice.
	raw, _ := fs.ReadFile(fsys, "history/merges.log")

	return protocol.BuildInfo{
		Version:   version,
		Commit:    commit,
		BuildTime: buildTime,
		Changes:   changelog.Parse(raw, changelog.Limit),
	}
}
