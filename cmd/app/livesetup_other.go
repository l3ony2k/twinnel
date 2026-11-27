//go:build !darwin

package main

// probeLiveSetup: Linux and Windows come later; nothing is detected.
func probeLiveSetup() liveSetup { return liveSetup{} }
