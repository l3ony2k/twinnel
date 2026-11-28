//go:build !windows

package windows

import (
	"context"
	"errors"

	"github.com/l3ony2k/twinnel/core/status"
)

// List reads the process list on Windows only.
func (l *ProcessLister) List(context.Context) ([]status.ProcessEntry, error) {
	return nil, errors.New("process list: only on Windows")
}
