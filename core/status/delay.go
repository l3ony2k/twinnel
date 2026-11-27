package status

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/l3ony2k/duet/core/mihomo"
)

// Delay tests. mihomo can test a whole group (GET /group/<name>/delay),
// but groups share their nodes, so testing group by group tests the same
// node again and again. The monitor tests nodes instead: each node at most
// once at a time (a second request joins the running test), at most
// DelayConcurrency at once, with the results kept by node name. Every
// group and tile showing a node reads that one result, and a group's delay
// is the delay of the node its selection resolves to. Built-ins (DIRECT,
// REJECT, ...) are never tested.

const (
	// DelayConcurrency is how many delay tests run at once.
	DelayConcurrency = 8
	// DelayFresh: a group test skips nodes tested this recently.
	DelayFresh = 10 * time.Second
	// delayTimeout is the test timeout passed to mihomo.
	delayTimeout = 5 * time.Second
)

// delayResult is the monitor's own last test of a node.
type delayResult struct {
	ms int // 0 = failed (timeout)
	at time.Time
}

// delayCall is one running test; whoever asks for the same node meanwhile
// waits for it instead of starting another.
type delayCall struct {
	done chan struct{}
	ms   int
	err  error
}

// delayState is the monitor's delay bookkeeping, guarded by Monitor.mu.
type delayState struct {
	proxies  map[string]mihomo.Proxy // the last /proxies answer (read-only)
	results  map[string]delayResult
	inflight map[string]*delayCall
	sem      chan struct{}

	batchRunning          bool
	batchDone, batchTotal int
}

func newDelayState() delayState {
	return delayState{
		results:  map[string]delayResult{},
		inflight: map[string]*delayCall{},
		sem:      make(chan struct{}, DelayConcurrency),
	}
}

// Testing reports whether a node of group (nested groups included) is
// being delay-tested, by any request.
func (m *Monitor) Testing(group string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.d.inflight) == 0 {
		return false
	}
	for _, n := range mihomo.Leaves(m.d.proxies, group) {
		if m.d.inflight[n] != nil {
			return true
		}
	}
	return false
}

// DelayProgress reports the running TestAll: nodes done and in all.
func (m *Monitor) DelayProgress() (done, total int, running bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.d.batchDone, m.d.batchTotal, m.d.batchRunning
}

// TestAll delay-tests every node reachable from any group, each once.
// Results show as they arrive. It returns when all are done or ctx ends
// (the tests already started finish in the background, bound to the
// monitor's run context). A second call while one runs returns at once.
func (m *Monitor) TestAll(ctx context.Context) error {
	base := m.runCtx()
	m.mu.Lock()
	if m.d.batchRunning {
		m.mu.Unlock()
		return nil
	}
	var names, urls []string
	seen := map[string]bool{}
	for _, g := range mihomo.Groups(m.d.proxies) {
		for _, n := range mihomo.Leaves(m.d.proxies, g.Name) {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
				urls = append(urls, m.testURL(g.TestURL))
			}
		}
	}
	m.d.batchRunning, m.d.batchDone, m.d.batchTotal = true, 0, len(names)
	calls := m.startLocked(base, names, urls)
	m.mu.Unlock()
	m.notify()

	err := m.wait(ctx, calls, func() {
		m.mu.Lock()
		m.d.batchDone++
		m.mu.Unlock()
	})
	m.mu.Lock()
	m.d.batchRunning = false
	m.mu.Unlock()
	m.pollMihomo(base, false)
	return err
}

// TestGroup delay-tests the nodes of group (nested groups opened), except
// those tested in the last DelayFresh. Nodes already being tested are
// waited for, not tested again.
func (m *Monitor) TestGroup(ctx context.Context, group string) error {
	base := m.runCtx()
	m.mu.Lock()
	url := ""
	if g, ok := m.d.proxies[group]; ok {
		url = g.TestURL
	}
	url = m.testURL(url)
	now := m.o.Now()
	var names, urls []string
	for _, n := range mihomo.Leaves(m.d.proxies, group) {
		if r, ok := m.d.results[n]; ok && m.d.inflight[n] == nil && now.Sub(r.at) < DelayFresh {
			continue
		}
		names = append(names, n)
		urls = append(urls, url)
	}
	calls := m.startLocked(base, names, urls)
	m.mu.Unlock()
	if len(calls) == 0 {
		return nil
	}
	m.notify()
	err := m.wait(ctx, calls, nil)
	m.pollMihomo(base, false)
	return err
}

// TestNode delay-tests one node (or joins its running test). Built-ins
// are not tested: (0, nil).
func (m *Monitor) TestNode(ctx context.Context, name string) (int, error) {
	base := m.runCtx()
	m.mu.Lock()
	if mihomo.IsBuiltin(name, m.d.proxies[name].Type) {
		m.mu.Unlock()
		return 0, nil
	}
	calls := m.startLocked(base, []string{name}, []string{m.testURL("")})
	m.mu.Unlock()
	m.notify()
	if err := m.wait(ctx, calls, nil); err != nil {
		return 0, err
	}
	m.pollMihomo(base, false)
	return calls[0].ms, calls[0].err
}

// testURL is the URL a test uses: the configured one, else the group's,
// else the client's default ("").
func (m *Monitor) testURL(group string) string {
	if m.o.TestURL != "" {
		return m.o.TestURL
	}
	return group
}

// runCtx is the context tests run under: the monitor's (they stop when
// the app goes away), not the caller's, as other callers may share them.
func (m *Monitor) runCtx() context.Context {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

// startLocked starts (or joins) a test per name. m.mu is held.
func (m *Monitor) startLocked(ctx context.Context, names, urls []string) []*delayCall {
	calls := make([]*delayCall, 0, len(names))
	for i, n := range names {
		c := m.d.inflight[n]
		if c == nil {
			c = &delayCall{done: make(chan struct{})}
			m.d.inflight[n] = c
			go m.runDelay(ctx, n, urls[i], c)
		}
		calls = append(calls, c)
	}
	return calls
}

func (m *Monitor) runDelay(ctx context.Context, name, url string, c *delayCall) {
	record := false
	select {
	case m.d.sem <- struct{}{}:
		c.ms, c.err = m.o.Mihomo.Delay(ctx, name, url, delayTimeout)
		<-m.d.sem
		// mihomo answered (a timeout is a 504/503 from it): a result.
		// A dropped connection, a wrong secret or the app quitting is not.
		var ae *mihomo.APIError
		record = c.err == nil || (errors.As(c.err, &ae) && !mihomo.IsUnauthorized(c.err) && ctx.Err() == nil)
		if c.err != nil {
			c.ms = 0
		}
	case <-ctx.Done():
		c.err = ctx.Err()
	}
	m.mu.Lock()
	delete(m.d.inflight, name)
	if record {
		m.d.results[name] = delayResult{ms: c.ms, at: m.o.Now()}
	}
	m.mu.Unlock()
	close(c.done)
	if record {
		m.regroup()
	} else {
		m.notify()
	}
}

// wait waits for calls (calling each, if set, as one ends) or ctx.
func (m *Monitor) wait(ctx context.Context, calls []*delayCall, each func()) error {
	var wg sync.WaitGroup
	for _, c := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-c.done:
				if each != nil {
					each()
					m.notify()
				}
			case <-ctx.Done():
			}
		}()
	}
	wg.Wait()
	return ctx.Err()
}

// regroup rebuilds the groups from the last /proxies answer and the delay
// results, so a result shows at once.
func (m *Monitor) regroup() {
	m.update(func(s *Snapshot) {
		if m.d.proxies == nil {
			return
		}
		s.Mihomo.Groups = buildGroups(m.d.proxies, m.d.results)
		s.Mihomo.Primary = PickPrimary(s.Mihomo.Groups, m.o.PrimaryGroup)
	})
}
