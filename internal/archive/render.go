package archive

import (
	"fmt"
	"strings"
)

type Transcript struct {
	Version  int       `json:"version"`
	Segments []Segment `json:"segments"`
}

type Segment struct {
	Speaker         string `json:"speaker"`
	OriginalSpeaker string `json:"original_speaker,omitempty"`
	StartMs         int64  `json:"start_ms"`
	EndMs           int64  `json:"end_ms"`
	Text            string `json:"text"`
}

func Render(tr Transcript, format string) ([]byte, error) {
	switch format {
	case "md":
		return renderMarkdown(tr), nil
	case "srt":
		return renderSRT(tr), nil
	case "vtt":
		return renderVTT(tr), nil
	case "txt":
		return renderText(tr), nil
	default:
		return nil, fmt.Errorf("unknown transcript format: %q", format)
	}
}

func renderMarkdown(tr Transcript) []byte {
	var b strings.Builder
	b.WriteString("# Transcript\n\n")
	for i, s := range tr.Segments {
		if s.Speaker != "" {
			fmt.Fprintf(&b, "**%s** [%s → %s]\n",
				s.Speaker, formatTimePeriod(s.StartMs), formatTimePeriod(s.EndMs))
			b.WriteString(s.Text)
			b.WriteString("\n")
		} else {
			fmt.Fprintf(&b, "[%s → %s]\n",
				formatTimePeriod(s.StartMs), formatTimePeriod(s.EndMs))
			b.WriteString(s.Text)
			b.WriteString("\n")
		}
		if i < len(tr.Segments)-1 {
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

func renderSRT(tr Transcript) []byte {
	var b strings.Builder
	for i, s := range tr.Segments {
		fmt.Fprintf(&b, "%d\n", i+1)
		fmt.Fprintf(&b, "%s --> %s\n",
			formatTimeComma(s.StartMs), formatTimeComma(s.EndMs))
		if s.Speaker != "" {
			fmt.Fprintf(&b, "%s: %s\n", s.Speaker, s.Text)
		} else {
			fmt.Fprintf(&b, "%s\n", s.Text)
		}
		if i < len(tr.Segments)-1 {
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

func renderVTT(tr Transcript) []byte {
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for i, s := range tr.Segments {
		fmt.Fprintf(&b, "%s --> %s\n",
			formatTimePeriod(s.StartMs), formatTimePeriod(s.EndMs))
		if s.Speaker != "" {
			fmt.Fprintf(&b, "%s: %s\n", s.Speaker, s.Text)
		} else {
			fmt.Fprintf(&b, "%s\n", s.Text)
		}
		if i < len(tr.Segments)-1 {
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

func renderText(tr Transcript) []byte {
	var b strings.Builder
	for i, s := range tr.Segments {
		if s.Speaker != "" {
			fmt.Fprintf(&b, "%s: %s", s.Speaker, s.Text)
		} else {
			b.WriteString(s.Text)
		}
		if i < len(tr.Segments)-1 {
			b.WriteString("\n\n")
		}
	}
	return []byte(b.String())
}

// formatTimePeriod formats milliseconds as HH:MM:SS.mmm (period decimal).
func formatTimePeriod(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	h := ms / 3600000
	rem := ms % 3600000
	m := rem / 60000
	rem = rem % 60000
	s := rem / 1000
	mmm := rem % 1000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, mmm)
}

// formatTimeComma formats milliseconds as HH:MM:SS,mmm (comma decimal, SRT).
func formatTimeComma(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	h := ms / 3600000
	rem := ms % 3600000
	m := rem / 60000
	rem = rem % 60000
	s := rem / 1000
	mmm := rem % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, mmm)
}
