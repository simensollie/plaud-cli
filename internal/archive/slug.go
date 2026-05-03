package archive

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	slugMaxLen         = 60
	slugBoundaryWindow = 10
	slugCollisionLen   = 6
	slugFallback       = "untitled"
)

var audioExtensions = []string{".mp3", ".m4a", ".wav", ".aac", ".flac", ".ogg"}

// Slug folds a recording title into the canonical slug form used by the
// per-recording folder name. F-03.
func Slug(title string) string {
	t := stripAudioExtension(title)
	t = foldNorwegian(t)
	t = norm.NFKD.String(t)
	t = stripCombiningMarks(t)
	t = strings.ToLower(t)
	t = nonWordToUnderscore(t)
	t = collapseUnderscores(t)
	t = strings.Trim(t, "_")
	if t == "" {
		return slugFallback
	}
	t = truncate(t)
	if t == "" {
		return slugFallback
	}
	return t
}

// SlugWithCollision returns Slug(title), but if the result would collide
// (per the caller's collide predicate) it appends an underscore plus up to
// slugCollisionLen characters of the recording ID. F-03.
func SlugWithCollision(title, id string, collide func(string) bool) string {
	base := Slug(title)
	if !collide(base) {
		return base
	}
	suffix := id
	if len(suffix) > slugCollisionLen {
		suffix = suffix[:slugCollisionLen]
	}
	return base + "_" + suffix
}

func stripAudioExtension(s string) string {
	lower := strings.ToLower(s)
	for _, ext := range audioExtensions {
		if strings.HasSuffix(lower, ext) {
			return s[:len(s)-len(ext)]
		}
	}
	return s
}

func foldNorwegian(s string) string {
	r := strings.NewReplacer(
		"æ", "ae", "Æ", "ae",
		"ø", "o", "Ø", "o",
		"å", "a", "Å", "a",
	)
	return r.Replace(s)
}

func stripCombiningMarks(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func nonWordToUnderscore(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
			continue
		}
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return b.String()
}

func collapseUnderscores(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prev := false
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			if !prev {
				b.WriteByte('_')
			}
			prev = true
			continue
		}
		b.WriteByte(s[i])
		prev = false
	}
	return b.String()
}

func truncate(s string) string {
	if len(s) <= slugMaxLen {
		return s
	}
	cut := s[:slugMaxLen]
	// Look for a `_` boundary inside the last slugBoundaryWindow chars.
	low := slugMaxLen - slugBoundaryWindow
	if low < 0 {
		low = 0
	}
	if idx := strings.LastIndex(cut, "_"); idx >= low {
		cut = cut[:idx]
	}
	cut = strings.TrimRight(cut, "_")
	return cut
}
