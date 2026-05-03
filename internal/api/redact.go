package api

import "regexp"

// Redaction patterns enforce F-13 across spec 0002 (download) and spec 0003
// (sync). Source-level scrub: every error returned to callers passes through
// RedactError before it can land in logs, state files, or NDJSON events.
// Surface-level scrub at sinks (state writer, event emitter, stderr printer)
// runs the same patterns again as defense in depth.

var (
	// urlPattern matches HTTP and HTTPS URLs up to the first whitespace or
	// quote. Strong default: replace the entire URL with a marker. Anything
	// the URL might leak (host, query, signed credentials, embedded tokens)
	// goes with it.
	urlPattern = regexp.MustCompile(`https?://[^\s"]+`)

	// signedQueryPattern matches the X-Amz-* credentials on signed S3 URLs
	// even when the URL is inside JSON or otherwise resists urlPattern.
	signedQueryPattern = regexp.MustCompile(`(?i)(X-Amz-(?:Signature|Credential|Security-Token|Date))=[^&"\s]+`)

	// jwtBearerPattern matches `Bearer eyJ...` JWT-shaped tokens.
	jwtBearerPattern = regexp.MustCompile(`(?i)Bearer\s+eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`)

	// jwtBarePattern catches standalone JWT tokens missing the Bearer prefix.
	jwtBarePattern = regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]{8,}`)

	// jsonTokenPattern matches `"token":"..."`, `"access_token":"..."`,
	// `"refresh_token":"..."`, `"set_password_token":"..."` style fields.
	jsonTokenPattern = regexp.MustCompile(`"((?:access_|refresh_|set_password_)?token)"\s*:\s*"[^"]*"`)
)

// RedactString returns s with every known credential-bearing pattern
// replaced by a marker. Idempotent; safe to apply more than once.
func RedactString(s string) string {
	s = urlPattern.ReplaceAllString(s, "<redacted-url>")
	s = signedQueryPattern.ReplaceAllString(s, "$1=REDACTED")
	s = jwtBearerPattern.ReplaceAllString(s, "Bearer REDACTED")
	s = jwtBarePattern.ReplaceAllString(s, "REDACTED")
	s = jsonTokenPattern.ReplaceAllString(s, `"$1":"REDACTED"`)
	return s
}

// RedactError returns the redacted err.Error() string, or "" if err is nil.
// Use this at every sink that might emit an error to user-visible output.
func RedactError(err error) string {
	if err == nil {
		return ""
	}
	return RedactString(err.Error())
}
