package health

// Saying why a fact could not be read: "access denied" is not "missing".
// A non-administrator run on Windows cannot read %ProgramData%\Twinnel,
// and a skip that said "not found" sent people looking for a file that
// was only locked.

import (
	"context"
	"errors"
	"io/fs"
)

// readSkip is a skip for a fact that could not be read, saying whether
// it was not there or not allowed (an access-denied file is not a
// missing one).
func readSkip(ctx context.Context, summary string, err error) Result {
	return Result{Status: StatusSkip, Summary: summary + autostartText(tx(ctx)).sep + accessWords(ctx, err), Evidence: []string{err.Error()}}
}

// accessWords says what kind of read failure err is, in ctx's language.
func accessWords(ctx context.Context, err error) string {
	A := autostartText(tx(ctx))
	switch {
	case errors.Is(err, fs.ErrPermission):
		return A.denied
	case errors.Is(err, fs.ErrNotExist):
		return A.notFound
	}
	return err.Error()
}

// skipMihomo is the skip of a check that could reach neither mihomo's
// controller nor its config file. It says which: the config not there,
// or there but not readable by this process (an administrators-only
// state dir); "not connected" alone sent people looking for a missing
// file that was only locked.
func skipMihomo(ctx context.Context, err error) Result {
	L := tx(ctx)
	A := autostartText(L)
	r := skipErr(L.msgMihomoUnavailable, err)
	switch {
	case errors.Is(err, fs.ErrPermission):
		r.Summary += A.sep + A.cfgDenied
	case errors.Is(err, fs.ErrNotExist):
		r.Summary += A.sep + A.cfgMissing
	}
	return r
}
