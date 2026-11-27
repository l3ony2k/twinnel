package main

import "testing"

func TestCLIFailureAfterPrompt(t *testing.T) {
	out := "# plan\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\nProceed? [y/N] duet: cancelled; nothing was changed\n"
	if got := cliFailure(out, nil); got != "cancelled; nothing was changed" {
		t.Fatalf("cliFailure = %q", got)
	}
	if got := cliFailure("boom", nil); got != "boom" {
		t.Fatalf("cliFailure plain = %q", got)
	}
}

func TestInstallArgsYes(t *testing.T) {
	args := installArgs("--yes")
	if args[len(args)-1] != "--yes" {
		t.Fatalf("installArgs = %v", args)
	}
}
