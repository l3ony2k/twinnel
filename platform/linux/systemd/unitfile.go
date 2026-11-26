package systemd

import (
	"strings"
)

// Unit is a parsed unit file: section → key → values in file order.
type Unit map[string]map[string][]string

// Get returns the last value of key in section ("" if unset).
func (u Unit) Get(section, key string) string {
	vs := u[section][key]
	if len(vs) == 0 {
		return ""
	}
	return vs[len(vs)-1]
}

// ParseUnitFile parses the INI-like unit file syntax: [Section] headers,
// Key=Value lines, # and ; comments, and backslash line continuations. An
// empty value resets the key's list, as systemd does for list settings.
// Drop-ins are not merged.
func ParseUnitFile(data []byte) Unit {
	u := Unit{}
	section := ""
	var cont strings.Builder
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if cont.Len() > 0 {
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";") {
				continue // comments inside continuations are skipped
			}
			line = cont.String() + " " + t
			cont.Reset()
		}
		if strings.HasSuffix(line, `\`) {
			cont.WriteString(strings.TrimSuffix(line, `\`))
			continue
		}
		t := strings.TrimSpace(line)
		switch {
		case t == "" || t[0] == '#' || t[0] == ';':
			continue
		case t[0] == '[' && t[len(t)-1] == ']':
			section = t[1 : len(t)-1]
			continue
		}
		k, v, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if u[section] == nil {
			u[section] = map[string][]string{}
		}
		if v == "" {
			u[section][k] = nil
			continue
		}
		u[section][k] = append(u[section][k], v)
	}
	return u
}

// SplitExec splits an Exec*= value into words: leading special prefixes
// (@ - : + !) are dropped, double and single quotes group words, and C-style
// backslash escapes, %% and $$ are decoded. Other specifiers (%n etc.) and
// $VAR references are left as they are.
func SplitExec(v string) []string {
	v = strings.TrimLeft(strings.TrimSpace(v), "@-:+!")
	var out []string
	var cur strings.Builder
	inWord := false
	var quote rune
	rs := []rune(v)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '"' || r == '\''):
			quote, inWord = r, true
		case r == '\\' && i+1 < len(rs):
			i++
			switch rs[i] {
			case 'n':
				cur.WriteRune('\n')
			case 't':
				cur.WriteRune('\t')
			default:
				cur.WriteRune(rs[i])
			}
			inWord = true
		case quote == 0 && (r == ' ' || r == '\t'):
			if inWord {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if inWord {
		out = append(out, cur.String())
	}
	for i, w := range out {
		out[i] = strings.NewReplacer("%%", "%", "$$", "$").Replace(w)
	}
	return out
}
