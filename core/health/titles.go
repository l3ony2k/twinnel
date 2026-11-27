package health

import "context"

// TitleOf returns the title of a built-in check in a language (a tag
// core/i18n matched), or "" for an unknown id. For clients that only have
// the id (the takeover result's before/after table).
func TitleOf(tag, id string) string {
	c, ok := DefaultRegistry().Get(id)
	if !ok {
		return ""
	}
	return checkTitle(tx(WithLang(context.Background(), tag)), c)
}
