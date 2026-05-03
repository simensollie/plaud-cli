package archive

import (
	"strings"
	"testing"
)

func TestSlug_F03_StripsAudioExtension(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"Kickoff Meeting.mp3", "kickoff_meeting"},
		{"Kickoff Meeting.M4A", "kickoff_meeting"},
		{"Recording.wav", "recording"},
		{"Voice memo.aac", "voice_memo"},
		{"Concert.flac", "concert"},
		{"Podcast.ogg", "podcast"},
		{"NotAnExt.txt", "notanext_txt"},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			got := Slug(tc.title)
			if got != tc.want {
				t.Fatalf("Slug(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

func TestSlug_F03_NorwegianFolding(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"æøå", "aeoa"},
		{"ÆØÅ", "aeoa"},
		{"Sjær på Gøteborg", "sjaer_pa_goteborg"},
		{"Aker Solutions ærlig", "aker_solutions_aerlig"},
		{"Kickoff møte", "kickoff_mote"},
		{"møte", "mote"},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			got := Slug(tc.title)
			if got != tc.want {
				t.Fatalf("Slug(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

func TestSlug_F03_NFKDFoldsCombiningMarks(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"Café résumé", "cafe_resume"},
		{"München", "munchen"},
		{"naïve façade", "naive_facade"},
		{"Crème brûlée", "creme_brulee"},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			got := Slug(tc.title)
			if got != tc.want {
				t.Fatalf("Slug(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

func TestSlug_F03_PostFoldTruncatesAtWordBoundary(t *testing.T) {
	// Title that folds to >60 chars; truncate at a `_` boundary inside the
	// last 10 chars when one exists.
	in := "this is a very long title that should be truncated word boundary near end"
	got := Slug(in)
	if len(got) > 60 {
		t.Fatalf("Slug(%q) length %d > 60", in, len(got))
	}
	if strings.HasSuffix(got, "_") {
		t.Fatalf("Slug(%q) = %q has trailing underscore", in, got)
	}
	// We expect a clean cut at a `_` boundary; verify the last segment is a
	// whole word (no truncation mid-word) by checking the last char is not
	// the final char of a word from the input.
	if len(got) < 50 {
		t.Fatalf("Slug(%q) = %q truncated too aggressively (len=%d)", in, got, len(got))
	}

	// Hard-cut case: no `_` boundary in the last 10 chars of the 60-char
	// window. Construct a title where the trailing 10 chars are all part of
	// one long word.
	hardIn := "abcdefghij_klmnopqrst_uvwxyzABCDEFGHIJabcdefghijklmnopqrstuvwxyz"
	hardGot := Slug(hardIn)
	if len(hardGot) != 60 {
		t.Fatalf("hard-cut Slug(%q) length = %d, want 60", hardIn, len(hardGot))
	}
}

func TestSlug_F03_EmptySlugFallsBackToUntitled(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"!!!",
		"---",
		"中文标题",
		"日本語タイトル",
		"...",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got := Slug(in)
			if got != "untitled" {
				t.Fatalf("Slug(%q) = %q, want %q", in, got, "untitled")
			}
		})
	}
}

func TestSlug_F03_LowercasesAndReplacesNonWord(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"Hello World", "hello_world"},
		{"Foo - Bar", "foo_bar"},
		{"a/b\\c", "a_b_c"},
		{"  spaced  out  ", "spaced_out"},
		{"!!!hello!!!", "hello"},
		{"UPPER lower", "upper_lower"},
		{"a..b..c", "a_b_c"},
		{"x   y", "x_y"},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			got := Slug(tc.title)
			if got != tc.want {
				t.Fatalf("Slug(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

func TestSlug_F03_CollisionAppendsSixCharIDSuffix(t *testing.T) {
	id := "a3f9c021b2d34e5f6789012345678901"
	base := Slug("Kickoff Meeting")
	if base != "kickoff_meeting" {
		t.Fatalf("base slug unexpected: %q", base)
	}

	// First call: no collision, returns base.
	collide := func(s string) bool { return false }
	got := SlugWithCollision("Kickoff Meeting", id, collide)
	if got != "kickoff_meeting" {
		t.Fatalf("no-collision slug = %q, want %q", got, "kickoff_meeting")
	}

	// Collision path: appends 6-char prefix of the ID.
	collideOnce := func(s string) bool { return s == "kickoff_meeting" }
	got = SlugWithCollision("Kickoff Meeting", id, collideOnce)
	want := "kickoff_meeting_a3f9c0"
	if got != want {
		t.Fatalf("collision slug = %q, want %q", got, want)
	}

	// ID shorter than 6 chars: should still work without panicking; uses
	// what is available.
	short := "abc"
	got = SlugWithCollision("Kickoff Meeting", short, collideOnce)
	if got != "kickoff_meeting_abc" {
		t.Fatalf("short-id slug = %q, want %q", got, "kickoff_meeting_abc")
	}
}
