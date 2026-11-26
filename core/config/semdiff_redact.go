package config

import (
	"net/url"
	"regexp"
	"strings"
)

// sensitiveKeyRE matches config keys whose values are credentials.
var sensitiveKeyRE = regexp.MustCompile(`(?i)(secret|password|passwd|token|authentication|auth-str|uuid|private-key|pre-shared-key|psk|obfs-password|sub-url|subscription)`)

// IsSensitiveKey reports whether values under key must never be printed.
func IsSensitiveKey(key string) bool { return sensitiveKeyRE.MatchString(key) }

var urlInTextRE = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"']+`)

// RedactURL hides credentials in a URL: user info and the query are replaced.
// With full set, the path is hidden too (subscription URLs carry the token in
// the path).
func RedactURL(s string, full bool) string {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return "<redacted>"
	}
	out := u.Scheme + "://"
	if u.User != nil {
		out += "<redacted>@"
	}
	out += u.Host
	switch {
	case full && (u.Path != "" || u.RawQuery != ""):
		out += "/<redacted>"
	case !full:
		out += u.EscapedPath()
		if u.RawQuery != "" {
			out += "?<redacted>"
		}
	}
	return out
}

// RedactText hides user info and query strings of URLs inside s.
func RedactText(s string) string {
	return urlInTextRE.ReplaceAllStringFunc(s, func(m string) string {
		u, err := url.Parse(m)
		if err != nil || (u.User == nil && u.RawQuery == "") {
			return m
		}
		return RedactURL(m, false)
	})
}

// redactValue renders a settings value for display under key.
func redactValue(key string, v any) string {
	if v == nil {
		return "<none>"
	}
	if IsSensitiveKey(key) {
		if s, ok := v.(string); ok && s == "" {
			return `""`
		}
		return "<redacted>"
	}
	return RedactText(compact(v))
}

func lastKey(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i+1:]
	}
	return path
}
