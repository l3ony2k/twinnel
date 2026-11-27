package health

import (
	"context"
	"errors"
	"fmt"
)

// A check that cannot run on this machine's platform is skipped with one
// plain line and Result.Unsupported set: never a warning, never counted as
// a problem, and it says the same thing every round. The app uses the flag
// to offer turning off a periodic test that can never run here.

// unsupported is the skip of a check that cannot run on platform.
func unsupported(L *texts, platform string) Result {
	return Result{Status: StatusSkip, Summary: fmt.Sprintf(L.msgNotOnPlatform, PlatformName(platform)), Unsupported: true}
}

// unsupportedErr returns the unsupported skip when err says a fact cannot
// be read on this platform (ErrNotSupported), else false.
func unsupportedErr(ctx context.Context, env Env, err error) (Result, bool) {
	if !errors.Is(err, ErrNotSupported) {
		return Result{}, false
	}
	return unsupported(tx(ctx), env.Platform()), true
}

// PlatformName is how a platform (runtime.GOOS) is written for users.
func PlatformName(goos string) string {
	switch goos {
	case Darwin:
		return "macOS"
	case Linux:
		return "Linux"
	case Windows:
		return "Windows"
	}
	return goos
}

// EgressSupporter is implemented by Envs that know at run time whether
// they can make the egress test's requests (Memo forwards it).
type EgressSupporter interface {
	EgressSupported() bool
}

// egressSupported reports whether env can run the egress test.
func egressSupported(env Env) bool {
	if s, ok := env.(EgressSupporter); ok {
		return s.EgressSupported()
	}
	_, ok := env.(EgressEnv)
	return ok
}
