package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/l3ony2k/twinnel/core/config"
	"github.com/l3ony2k/twinnel/core/rpc"
	"github.com/l3ony2k/twinnel/core/store"
	"gopkg.in/yaml.v3"
)

const maxSubscription = 32 << 20

func (e *Engine) subscriptionInfos(st *store.Settings) []rpc.Subscription {
	out := make([]rpc.Subscription, 0, len(st.Subscriptions))
	for _, s := range st.Subscriptions {
		_, err := e.Secrets.Get(store.SubscriptionURLKey(s.ID))
		out = append(out, rpc.Subscription{
			ID: s.ID, Name: s.Name, Active: s.Active, HasURL: err == nil,
			IntervalMinutes: s.IntervalMinutes, LastFetch: s.LastFetch, LastChange: s.LastChange,
			Hash: s.Hash, Size: s.Size, LastError: s.LastError,
		})
	}
	return out
}

// Subscriptions lists subscriptions (never their URLs).
func (e *Engine) Subscriptions() ([]rpc.Subscription, error) {
	st, err := e.Store.Settings()
	if err != nil {
		return nil, err
	}
	return e.subscriptionInfos(st), nil
}

func checkSubscriptionURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return rpc.ErrInvalid("subscription URL does not parse")
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return rpc.ErrInvalid("subscription URL must be http(s)")
	}
	return nil
}

// checkSubscriptionContent makes sure the content is a mihomo/Clash config
// with nodes; a provider error page or a base64 node list is refused.
func checkSubscriptionContent(b []byte) error {
	root, err := config.Parse(b)
	if err != nil {
		return fmt.Errorf("not a YAML config: %v", err)
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		switch root.Content[i].Value {
		case "proxies", "proxy-providers":
			if root.Content[i+1].Kind == yaml.SequenceNode || root.Content[i+1].Kind == yaml.MappingNode {
				return nil
			}
		}
	}
	return errors.New("not a mihomo/Clash config: no proxies or proxy-providers")
}

// PutSubscription creates or edits a subscription. The first subscription
// becomes active.
func (e *Engine) PutSubscription(ctx context.Context, p rpc.SubPutParams) (rpc.Subscription, error) {
	if !store.ValidID(p.ID) {
		return rpc.Subscription{}, rpc.ErrInvalid("invalid id %q", p.ID)
	}
	if p.URL != "" {
		if err := checkSubscriptionURL(p.URL); err != nil {
			return rpc.Subscription{}, err
		}
	}
	if p.Content != "" {
		if err := checkSubscriptionContent([]byte(p.Content)); err != nil {
			return rpc.Subscription{}, rpc.ErrInvalid("%v", err)
		}
	}
	if p.IntervalMinutes != nil && (*p.IntervalMinutes < 0 || (*p.IntervalMinutes > 0 && *p.IntervalMinutes < 5)) {
		return rpc.Subscription{}, rpc.ErrInvalid("interval_minutes must be 0 (manual) or at least 5")
	}
	if p.URL != "" {
		if err := e.Secrets.Set(store.SubscriptionURLKey(p.ID), p.URL); err != nil {
			return rpc.Subscription{}, err
		}
	}
	now := e.now().UTC()
	if p.Content != "" {
		if err := e.Store.WriteSubscription(p.ID, []byte(p.Content)); err != nil {
			return rpc.Subscription{}, err
		}
	}
	st, err := e.Store.UpdateSettings(func(st *store.Settings) error {
		i := st.SubscriptionIndex(p.ID)
		if i < 0 {
			st.Subscriptions = append(st.Subscriptions, store.Subscription{ID: p.ID, Name: p.ID, Active: len(st.Subscriptions) == 0, IntervalMinutes: 1440})
			i = len(st.Subscriptions) - 1
		}
		s := &st.Subscriptions[i]
		if p.Name != "" {
			s.Name = p.Name
		}
		if p.IntervalMinutes != nil {
			s.IntervalMinutes = *p.IntervalMinutes
		}
		if p.Content != "" {
			h := store.Hash([]byte(p.Content))
			if h != s.Hash {
				s.LastChange = now
			}
			s.Hash, s.Size, s.LastFetch, s.LastError = h, len(p.Content), now, ""
		}
		return nil
	})
	if err != nil {
		return rpc.Subscription{}, err
	}
	e.publish(rpc.EventStateChanged, map[string]string{"subscription": p.ID, "op": "saved", "name": p.Name})
	for _, s := range e.subscriptionInfos(st) {
		if s.ID == p.ID {
			if w := plainHTTPWarning(p.URL); w != "" {
				s.Warnings = append(s.Warnings, w)
			}
			return s, nil
		}
	}
	return rpc.Subscription{}, errors.New("subscription vanished")
}

// DeleteSubscription removes a subscription, its content and its URL.
func (e *Engine) DeleteSubscription(id string) error {
	var name string
	_, err := e.Store.UpdateSettings(func(st *store.Settings) error {
		i := st.SubscriptionIndex(id)
		if i < 0 {
			return rpc.ErrNotFound("subscription", id)
		}
		if st.Subscriptions[i].Active {
			return rpc.ErrInvalid("subscription %q is active; activate another one first", id)
		}
		name = st.Subscriptions[i].Name
		st.Subscriptions = append(st.Subscriptions[:i], st.Subscriptions[i+1:]...)
		return nil
	})
	if err != nil {
		return err
	}
	e.Store.DeleteSubscription(id)
	e.Secrets.Delete(store.SubscriptionURLKey(id))
	e.publish(rpc.EventStateChanged, map[string]string{"subscription": id, "op": "deleted", "name": name})
	return nil
}

// ActivateSubscription makes id the active subscription (no apply).
func (e *Engine) ActivateSubscription(id string) error {
	_, err := e.Store.UpdateSettings(func(st *store.Settings) error {
		if st.SubscriptionIndex(id) < 0 {
			return rpc.ErrNotFound("subscription", id)
		}
		for i := range st.Subscriptions {
			st.Subscriptions[i].Active = st.Subscriptions[i].ID == id
		}
		return nil
	})
	if err == nil {
		e.publish(rpc.EventStateChanged, map[string]string{"subscription": id, "op": "activated"})
	}
	return err
}

// downloadClient picks the proxy: mihomo's mixed port if mihomo answers,
// else direct.
func (e *Engine) downloadClient(ctx context.Context, st *store.Settings) (*http.Client, string) {
	vctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	_, err := e.currentController(st).Version(vctx)
	cancel()
	if err == nil {
		port := st.Mihomo.MixedPort
		if port == 0 {
			if b, err := readConfined(st.Mihomo.ConfigFile); err == nil {
				port = mixedPort(b)
			}
		}
		if port > 0 {
			return e.httpClient(&url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(port)}), "proxy"
		}
	}
	return e.httpClient(nil), "direct"
}

// RefreshSubscription downloads a subscription. On a content change it is
// stored, and if it is the active one (and auto-apply is on) the config is
// composed and applied again. The URL never appears in errors or logs.
func (e *Engine) RefreshSubscription(ctx context.Context, id string) (rpc.SubRefreshResult, error) {
	var out rpc.SubRefreshResult
	st, err := e.Store.Settings()
	if err != nil {
		return out, err
	}
	i := st.SubscriptionIndex(id)
	if i < 0 {
		return out, rpc.ErrNotFound("subscription", id)
	}
	sub := st.Subscriptions[i]
	rawURL, err := e.Secrets.Get(store.SubscriptionURLKey(id))
	if err != nil {
		return out, rpc.Errorf(rpc.CodeFailed, "subscription %q has no URL", id)
	}
	cl, via := e.downloadClient(ctx, st)
	out.Via = via
	body, err := e.fetch(ctx, cl, rawURL)
	if err == nil {
		err = checkSubscriptionContent(body)
	}
	now := e.now().UTC()
	if err != nil {
		e.Store.UpdateSettings(func(st *store.Settings) error {
			if j := st.SubscriptionIndex(id); j >= 0 {
				st.Subscriptions[j].LastError = err.Error()
			}
			return nil
		})
		e.publish(rpc.EventSubFailed, map[string]string{"id": id, "error": err.Error()})
		return out, rpc.Errorf(rpc.CodeFailed, "subscription %s: %v", id, err)
	}
	out.Hash = store.Hash(body)
	out.Changed = out.Hash != sub.Hash
	if out.Changed {
		if err := e.Store.WriteSubscription(id, body); err != nil {
			return out, err
		}
	}
	st, err = e.Store.UpdateSettings(func(st *store.Settings) error {
		if j := st.SubscriptionIndex(id); j >= 0 {
			s := &st.Subscriptions[j]
			s.LastFetch, s.LastError, s.Hash, s.Size = now, "", out.Hash, len(body)
			if out.Changed {
				s.LastChange = now
			}
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	e.publish(rpc.EventSubUpdated, map[string]any{"id": id, "changed": out.Changed, "via": via})
	if out.Changed && sub.Active && *st.AutoApply && st.Drift == nil && !e.adopting.Load() {
		r, _ := e.Apply(ctx, rpc.ApplyParams{Reason: "subscription " + id + " updated", ReasonCode: store.ReasonSubUpdated, ReasonArgs: []string{id}})
		out.Apply = r
	}
	return out, nil
}

// fetch GETs the URL; errors are rewritten so they don't contain it (the
// URL carries the subscription token).
func (e *Engine) fetch(ctx context.Context, cl *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("bad subscription URL")
	}
	ua := e.UserAgent
	if ua == "" {
		ua = "clash.meta"
	}
	req.Header.Set("User-Agent", ua)
	resp, err := cl.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			return nil, fmt.Errorf("download failed: %v", redact(ue.Err, rawURL))
		}
		return nil, fmt.Errorf("download failed: %v", redact(err, rawURL))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscription+1))
	if err != nil {
		return nil, fmt.Errorf("download failed: %v", redact(err, rawURL))
	}
	if len(b) > maxSubscription {
		return nil, errors.New("download failed: subscription larger than 32 MiB")
	}
	return b, nil
}

func redact(err error, secret string) string {
	s := err.Error()
	if secret != "" {
		s = strings.ReplaceAll(s, secret, "<subscription-url>")
		if u, perr := url.Parse(secret); perr == nil && u.RawQuery != "" {
			s = strings.ReplaceAll(s, u.RawQuery, "<redacted>")
		}
	}
	return s
}

func (e *Engine) subscriptionLoop(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		e.refreshDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// refreshDue refreshes every subscription whose interval has passed.
func (e *Engine) refreshDue(ctx context.Context) {
	st, err := e.Store.Settings()
	if err != nil {
		return
	}
	now := e.now()
	for _, s := range st.Subscriptions {
		if s.IntervalMinutes <= 0 {
			continue
		}
		if _, err := e.Secrets.Get(store.SubscriptionURLKey(s.ID)); err != nil {
			continue
		}
		next := s.LastFetch.Add(time.Duration(s.IntervalMinutes) * time.Minute)
		if s.LastError != "" && !s.LastFetch.IsZero() {
			// After a failure retry sooner, but not every minute.
			next = s.LastFetch.Add(min(time.Duration(s.IntervalMinutes)*time.Minute, 30*time.Minute))
		}
		if now.Before(next) {
			continue
		}
		if _, err := e.RefreshSubscription(ctx, s.ID); err != nil {
			e.log().Warn("scheduled subscription refresh", "id", s.ID, "err", err)
			// Count the failed attempt so the retry waits.
			e.Store.UpdateSettings(func(st *store.Settings) error {
				if j := st.SubscriptionIndex(s.ID); j >= 0 {
					st.Subscriptions[j].LastFetch = now.UTC()
				}
				return nil
			})
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// plainHTTPWarning warns about a subscription URL without TLS (security
// review finding 14): anyone on the network path can read the URL (often a
// credential) and serve a config of their choosing. Such a config is still
// bounded by exposureCheck and mihomo -t, so it is a warning, not a refusal.
func plainHTTPWarning(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "http") {
		return ""
	}
	return "the subscription URL uses plain http: anyone on the network path can read it and replace the config it returns; use https if the provider offers it"
}
