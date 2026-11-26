package health

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func statusLabel(s Status) string {
	switch s {
	case StatusOK:
		return labelOK
	case StatusWarn:
		return labelWarn
	case StatusFail:
		return labelFail
	}
	return labelSkip
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

func countsLine(r *Report) string {
	c := r.Counts()
	return fmt.Sprintf(msgCounts, c[StatusOK], c[StatusWarn], c[StatusFail], c[StatusSkip])
}

// Markdown renders the report as Markdown, for pasting into an issue or a
// chat with an AI.
func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", titleReport)
	fmt.Fprintf(&b, "- %s: %s %s\n", labelOverall, statusIcon(r.Overall), statusLabel(r.Overall))
	fmt.Fprintf(&b, "- %s: %s\n", labelTime, r.Started.Format(time.RFC3339))
	fmt.Fprintf(&b, "- %s: %s\n", labelPlatform, r.Platform)
	fmt.Fprintf(&b, "- %s\n\n", countsLine(r))
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "## %s %s (`%s`)\n\n", statusIcon(c.Status), c.Title, c.ID)
		fmt.Fprintf(&b, "**%s**: %s\n\n", statusLabel(c.Status), c.Summary)
		if len(c.Evidence) > 0 {
			fmt.Fprintf(&b, "%s:\n\n```\n", labelEvidence)
			for _, e := range c.Evidence {
				b.WriteString(e)
				b.WriteByte('\n')
			}
			b.WriteString("```\n\n")
		}
		if c.Fix != nil {
			fmt.Fprintf(&b, "%s: %s\n\n", labelFix, c.Fix.Text)
			if c.Fix.Command != "" {
				fmt.Fprintf(&b, "```sh\n%s\n```\n\n", c.Fix.Command)
			}
		}
	}
	return b.String()
}

// Text renders the report as plain text for a terminal.
func (r *Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s: %s  (%s)\n", titleReport, labelOverall, statusLabel(r.Overall), countsLine(r))
	fmt.Fprintf(&b, "%s: %s  %s: %s\n\n", labelTime, r.Started.Format("2006-01-02 15:04:05"), labelPlatform, r.Platform)
	for _, c := range r.Checks {
		fmt.Fprintf(&b, "[%-4s] %s (%s)\n", strings.ToUpper(string(c.Status)), c.Title, c.ID)
		fmt.Fprintf(&b, "       %s\n", c.Summary)
		for _, e := range c.Evidence {
			for _, line := range strings.Split(e, "\n") {
				fmt.Fprintf(&b, "         · %s\n", line)
			}
		}
		if c.Fix != nil {
			fmt.Fprintf(&b, "       %s: %s\n", labelFix, c.Fix.Text)
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
