//go:build windows

package scm

import "github.com/l3ony2k/twinnel/core/service"

// Every plan a Windows program prints reads in Windows terms.
func init() { service.SetDialect(Dialect) }
