package main

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/l3ony2k/duet/core/health"
	"github.com/l3ony2k/duet/core/i18n"
	"github.com/l3ony2k/duet/core/rpc"
	"github.com/l3ony2k/duet/core/rpc/healthsrc"
	"github.com/l3ony2k/duet/core/status"
)

// helperHealth is the app's health source. While the helper is connected
// it answers from the helper's scheduler (health.last; health.run only
// when the user asks for a rerun), so the app does not run the checks a
// second time. Without a helper, or against a helper too old to have
// health.*, it runs them in-process (local).
type helperHealth struct {
	hc    helperCaller
	local status.HealthSource

	fresh     atomic.Bool // next Run asks the helper for a new run
	fromHelpr atomic.Bool // the last report came from the helper
}

func (s *helperHealth) Run(ctx context.Context) (*health.Report, error) {
	fresh := s.fresh.Swap(false)
	if s.hc != nil && s.hc.State().Connected() {
		rep, err := healthsrc.Source{Client: s.hc, Fresh: fresh, Reason: "app", Lang: i18n.Tag()}.Run(ctx)
		var re *rpc.Error
		if err == nil || !errors.As(err, &re) || re.Code != rpc.CodeMethodNotFound {
			s.fromHelpr.Store(err == nil)
			return rep, err
		}
		// An older helper without health.*: run the checks here.
	}
	s.fromHelpr.Store(false)
	return s.local.Run(ctx)
}

// helperCaller is the part of *status.HelperConn helperHealth uses.
type helperCaller interface {
	State() status.Helper
	Call(ctx context.Context, method string, params, result any) error
}

// FromHelper reports whether the last report came from the helper.
func (s *helperHealth) FromHelper() bool { return s.fromHelpr.Load() }

// wrapHealth puts the helper in front of a local source; nil stays nil
// (platforms without checks).
func wrapHealth(hc *status.HelperConn, local status.HealthSource) *helperHealth {
	if local == nil {
		return nil
	}
	h := &helperHealth{local: local}
	if hc != nil {
		h.hc = hc
	}
	return h
}

// healthEvent reports whether a batch has a new helper health result.
func healthEvent(evs []rpc.Event) bool {
	for _, e := range evs {
		if e.Type == rpc.EventCheckChanged || e.Type == rpc.EventHealthUpdated {
			return true
		}
	}
	return false
}
