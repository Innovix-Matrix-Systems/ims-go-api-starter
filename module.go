// Package project exposes files from the repository root to the binary
// (the developer dashboard's Inspector lists the Go modules from go.mod).
package project

import _ "embed"

// GoMod is the module's go.mod.
//
//go:embed go.mod
var GoMod []byte
