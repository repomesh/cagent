//go:build js && wasm

package mcp

import "os"

func lockExclusive(_ *os.File) error { return nil }

func unlockFile(_ *os.File) error { return nil }
