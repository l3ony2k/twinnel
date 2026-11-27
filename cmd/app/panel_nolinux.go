//go:build !linux

package main

// The tray panel as a window and the tray host check are Linux's
// (panel_linux.go); elsewhere the panel is a flyout of the tray icon.

func (a *app) openPanelWindow() {}

func (a *app) checkTrayHost() {}
