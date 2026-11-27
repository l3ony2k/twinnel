package health

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func statusLabel(L *texts, s Status) string {
	switch s {
	case StatusOK:
		return L.labelOK
	case StatusWarn:
		return L.labelWarn
	case StatusFail:
		return L.labelFail
	}
	return L.labelSkip
}

func statusIcon(s Status) string {
	switch s {
	case StatusOK:
		return "✅"
	case StatusWarn:
		return "⚠️"
	case StatusFail:
		return "❌"
	}
	return "⏭️"
}

func countsLine(L *texts, r *Report) string {
	c := r.Counts()
	return fmt.Sprintf(L.msgCounts, c[StatusOK], c[StatusWarn], c[StatusFail], c[StatusSkip])
}

// sourceNote says that a report is not the helper's ("" when it is, or
// does not say).
func sourceNote(L *texts, source string) string {
	switch source {
	case SourceLocal:
		return L.msgSourceLocal
	case SourceLocalNoHelper:
		return L.msgSourceNoHelper
	}
	return ""
}

// Markdown renders the report as Markdown, for pasting into an issue or a
// chat with an AI.
func (r *Report) Markdown() string {
	L := textFor(r.Lang)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", L.titleReport)
	fmt.Fprintf(&b, "- %s: %s %s\n", L.labelOverall, statusIcon(r.Overall), statusLabel(L, r.Overall))
	fmt.Fprintf(&b, "- %s: %s\n", L.labelTime, r.Started.Format(time.RFC3339))
	fmt.Fprintf(&b, "- %s: %s\n", L.labelPlatform, r.Platform)
	fmt.Fprintf(&b, "- %s\n", countsLine(L, r))
	if note := sourceNote(L, r.Source); note != "" {
		fmt.Fprintf(&b, "- %s\n", note)
	}
	b.WriteByte('\n')
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "## %s %s (`%s`)\n\n", statusIcon(c.Status), c.Title, c.ID)
		fmt.Fprintf(&b, "**%s**: %s\n\n", statusLabel(L, c.Status), c.Summary)
		if c.Controller != "" {
			fmt.Fprintf(&b, "%s: %s\n\n", otx(L).labelController, c.Controller)
		}
		if len(c.Evidence) > 0 {
			fmt.Fprintf(&b, "%s:\n\n```\n", L.labelEvidence)
			for _, e := range c.Evidence {
				b.WriteString(e)
				b.WriteByte('\n')
			}
			b.WriteString("```\n\n")
		}
		if c.Fix != nil {
			fmt.Fprintf(&b, "%s: %s\n\n", L.labelFix, c.Fix.Text)
			if c.Fix.Command != "" {
				fmt.Fprintf(&b, "```sh\n%s\n```\n\n", c.Fix.Command)
			}
		}
	}
	return b.String()
}

// Text renders the report as plain text for a terminal.
func (r *Report) Text() string {
	L := textFor(r.Lang)
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s: %s  (%s)\n", L.titleReport, L.labelOverall, statusLabel(L, r.Overall), countsLine(L, r))
	fmt.Fprintf(&b, "%s: %s  %s: %s\n", L.labelTime, r.Started.Format("2006-01-02 15:04:05"), L.labelPlatform, r.Platform)
	if note := sourceNote(L, r.Source); note != "" {
		fmt.Fprintf(&b, "%s\n", note)
	}
	b.WriteByte('\n')
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "[%-4s] %s (%s)\n", strings.ToUpper(string(c.Status)), c.Title, c.ID)
		fmt.Fprintf(&b, "       %s\n", c.Summary)
		if c.Controller != "" {
			fmt.Fprintf(&b, "       %s: %s\n", otx(L).labelController, c.Controller)
		}
		for _, e := range c.Evidence {
			for _, line := range strings.Split(e, "\n") {
				fmt.Fprintf(&b, "         · %s\n", line)
			}
		}
		if c.Fix != nil {
			fmt.Fprintf(&b, "       %s: %s\n", L.labelFix, c.Fix.Text)
			if c.Fix.Command != "" {
				for _, line := range strings.Split(c.Fix.Command, "\n") {
					fmt.Fprintf(&b, "         $ %s\n", line)
				}
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// JSON renders the report as indented JSON.
func (r *Report) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
