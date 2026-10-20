//go:build !darwin || !cgo

package main

// The status item is macOS-only.
const menuBarSupported = false

func menuBarCreate([]byte, []byte, string, string, string) {}

func menuBarRender(menuBarStatus) {}

func menuBarRenderOverlay(string) {}
