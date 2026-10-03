//go:build !unix

package config

// RaiseFileLimit does nothing where there is no per-process descriptor limit to raise.
func RaiseFileLimit(uint64) error { return nil }
