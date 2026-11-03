// Package catalog holds mtm's app definitions, one TOML file per app,
// built into the binary. internal/catalog reads and checks them; the
// format is described in README.md here.
package catalog

import "embed"

//go:embed *.toml
var Files embed.FS
