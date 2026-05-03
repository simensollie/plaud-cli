//go:build windows

package archive

import (
	"path/filepath"
	"strings"
)

// PrefixLongPath returns p prefixed with the Windows extended-length syntax
// (\\?\) so callers can exceed the 260-char MAX_PATH limit. UNC inputs
// (\\server\share\...) are rewritten as \\?\UNC\server\share\... per the
// Win32 file namespace rules.
func PrefixLongPath(p string) string {
	if strings.HasPrefix(p, `\\?\`) {
		return p
	}
	if strings.HasPrefix(p, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(p, `\\`)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	return `\\?\` + abs
}
