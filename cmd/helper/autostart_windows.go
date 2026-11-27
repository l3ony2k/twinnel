package main

import (
	"github.com/l3ony2k/twinnel/core/autostart"
	"github.com/l3ony2k/twinnel/platform/windows"
)

// autostartSource reads the Task Scheduler's tasks and the Run keys.
func autostartSource(o *options) autostart.Source {
	if o.takeoverServices != "" {
		return &autostart.TaskDirSource{Dir: o.takeoverServices} // development: a scratch dir of task files
	}
	return windows.AutostartSource()
}
