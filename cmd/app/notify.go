package main

import (
	"context"
	"log"
	"time"

	"github.com/egoist/mygo"

	"github.com/l3ony2k/duet/core/status"
)

// notifier shows the status.Alerter's alerts as desktop notifications.
// Main thread, except the Show calls, which wait for the system.
type notifier struct {
	alerter status.Alerter
	denied  bool
}

func newNotifier() *notifier { return &notifier{} }

// supported reports whether this run can show notifications (macOS shows
// them only for a packaged app).
func notificationsSupported() bool { return mygo.NotificationsSupported() }

// check feeds a snapshot to the alerter and shows what it returns; a click
// calls open with the alert's key.
func (n *notifier) check(s status.Snapshot, open func(key string)) {
	alerts := n.alerter.Check(s)
	if len(alerts) == 0 || n.denied || !notificationsSupported() {
		return
	}
	for _, al := range alerts {
		note := mygo.NewNotification(mygo.NotificationOptions{
			ID:    "duet/" + al.Key,
			Group: "duet",
			Title: al.Title,
			Body:  al.Body,
		})
		key := al.Key
		note.OnClick(func() {
			mygo.RunOnMain(func() { open(key) })
			note.Close()
		})
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := note.ShowContext(ctx); err != nil {
				log.Printf("notification %s: %v", key, err)
				if err == mygo.ErrNotificationsDenied {
					mygo.RunOnMain(func() { n.denied = true })
				}
			}
		}()
	}
}
