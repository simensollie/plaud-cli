//go:build !windows

package archive

// PrefixLongPath is a no-op on POSIX. The function exists so callers do not
// need runtime.GOOS branches at every archive-write site.
func PrefixLongPath(p string) string {
	return p
}
