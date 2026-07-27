// Package ruleassets owns the trusted rule documents embedded in WindowsAgent.
package ruleassets

import "embed"

// Documents contains each rule folder's canonical AGENTS.md.
//
//go:embed */AGENTS.md
var Documents embed.FS
