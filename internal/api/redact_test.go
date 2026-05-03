package api

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRedact_F13_StripsSignedURLQueryString(t *testing.T) {
	in := `GET https://euc1-prod-plaud-bucket.s3.amazonaws.com/audio/abc.mp3?` +
		`X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIASECRET&` +
		`X-Amz-Date=20260503T140000Z&X-Amz-Signature=deadbeef1234&` +
		`X-Amz-Expires=3600 returned 403`
	got := RedactString(in)
	for _, leak := range []string{"AKIASECRET", "deadbeef1234", "X-Amz-Signature=deadbeef", "X-Amz-Credential=AKIA"} {
		if strings.Contains(got, leak) {
			t.Fatalf("F-13: leak detected after redaction\nin:  %q\nout: %q\nleak: %q", in, got, leak)
		}
	}
	if !strings.Contains(got, "REDACTED") && !strings.Contains(got, "<redacted") {
		t.Fatalf("expected a redaction marker in %q", got)
	}
}

func TestRedact_F13_StripsBearerJWT(t *testing.T) {
	in := `unauthorized: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.abc-123_xyz returned 401`
	got := RedactString(in)
	if strings.Contains(got, "eyJ") {
		t.Fatalf("F-13: JWT survived redaction: %q", got)
	}
	if !strings.Contains(got, "Bearer REDACTED") {
		t.Fatalf("F-13: expected 'Bearer REDACTED' in %q", got)
	}
}

func TestRedact_F13_StripsJSONTokenField(t *testing.T) {
	in := `decoding response: invalid char at offset 42 near {"token":"sk-secret-abc","next":1}`
	got := RedactString(in)
	if strings.Contains(got, "sk-secret-abc") {
		t.Fatalf("F-13: token field leaked: %q", got)
	}

	in2 := `{"access_token":"AAAA-BBBB-CCCC","expires_in":3600}`
	got2 := RedactString(in2)
	if strings.Contains(got2, "AAAA-BBBB-CCCC") {
		t.Fatalf("F-13: access_token field leaked: %q", got2)
	}
}

func TestRedact_F13_StripsHTTPSURL(t *testing.T) {
	in := `calling https://api.plaud.ai/file/temp-url/abc?token=secret: timeout`
	got := RedactString(in)
	if strings.Contains(got, "api.plaud.ai") || strings.Contains(got, "secret") {
		t.Fatalf("F-13: URL not redacted: %q", got)
	}
}

func TestRedact_F13_PassesThroughBenignText(t *testing.T) {
	cases := []string{
		"recording 1234 not found",
		"transcript not yet ready, skipped",
		"refusing to prune: server returned 0 recordings",
	}
	for _, in := range cases {
		got := RedactString(in)
		if got != in {
			t.Errorf("benign text mutated\nin:  %q\nout: %q", in, got)
		}
	}
}

func TestRedact_F13_NilErrorReturnsEmpty(t *testing.T) {
	if got := RedactError(nil); got != "" {
		t.Fatalf("RedactError(nil) = %q, want empty", got)
	}
}

func TestRedact_F13_RedactErrorMatchesRedactString(t *testing.T) {
	err := errors.New("Bearer eyJabc.def.ghi failed")
	want := RedactString(err.Error())
	got := RedactError(err)
	if got != want {
		t.Fatalf("RedactError mismatch\nstring: %q\nerror : %q", want, got)
	}
}

func TestRedact_F13_WrappedErrorPreservesChain(t *testing.T) {
	inner := errors.New("Bearer eyJabc.def.ghi")
	outer := fmt.Errorf("calling /api: %w", inner)
	got := RedactError(outer)
	if strings.Contains(got, "eyJ") {
		t.Fatalf("F-13: wrapped JWT survived: %q", got)
	}
	if !strings.Contains(got, "calling /api:") {
		t.Fatalf("expected wrapping context preserved in %q", got)
	}
}
