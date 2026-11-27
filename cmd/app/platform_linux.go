//go:build linux

package main

import (
	"net/http"
	"net/url"
)

// systemProxy is the update check's proxy: the environment's (Linux
// desktops export http_proxy/https_proxy; GNOME's and KDE's proxy
// settings are not read).
func systemProxy() func(*http.Request) (*url.URL, error) { return http.ProxyFromEnvironment }

// liquidGlass is macOS 26's material: nowhere else.
const liquidGlass = false
