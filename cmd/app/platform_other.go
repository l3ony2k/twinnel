//go:build !darwin && !linux

package main

import (
	"net/http"
	"net/url"
)

// systemProxy is the update check's proxy: the environment's.
func systemProxy() func(*http.Request) (*url.URL, error) { return http.ProxyFromEnvironment }

// liquidGlass is macOS 26's material: nowhere else.
const liquidGlass = false
