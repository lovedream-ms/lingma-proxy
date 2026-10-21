//go:build !windows

package autostart

func newWindowsAutostarter(string, string) Autostarter { return &dummyAutostarter{} }
