package main

import (
	"fmt"
	"testing"

	"github.com/l3ony2k/twinnel/core/rpc"
)

func TestErrorText(t *testing.T) {
	re := &rpc.Error{Code: rpc.CodeNotFound, Message: `layer "nosuch" not found`}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{re, `layer "nosuch" not found`},
		{fmt.Errorf("layer get: %w", re), `layer get: layer "nosuch" not found`},
		{fmt.Errorf("plain"), "plain"},
	} {
		if got := errorText(tc.err); got != tc.want {
			t.Errorf("%v: got %q", tc.err, got)
		}
	}
}
