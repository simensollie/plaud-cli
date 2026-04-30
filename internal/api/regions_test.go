package api

import (
	"errors"
	"testing"
)

// TestRegions_F01_BaseURLPerRegion pins the region-to-host mapping that the
// rest of the package relies on. Plaud's backend is regional; using the wrong
// host returns 401 with no useful error, so this mapping is load-bearing.
//
// Spec: specs/0001-auth-and-list/ F-01
func TestRegions_F01_BaseURLPerRegion(t *testing.T) {
	cases := []struct {
		name    string
		region  Region
		want    string
		wantErr error
	}{
		{name: "us", region: RegionUS, want: "https://api.plaud.ai"},
		{name: "eu", region: RegionEU, want: "https://api-euc1.plaud.ai"},
		{name: "jp", region: RegionJP, want: "https://api-jp.plaud.ai"},
		{name: "unknown", region: Region("xx"), wantErr: ErrUnknownRegion},
		{name: "empty", region: Region(""), wantErr: ErrUnknownRegion},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BaseURL(tc.region)

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("BaseURL(%q) err = %v, want %v", tc.region, err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("BaseURL(%q) unexpected error: %v", tc.region, err)
			}
			if got != tc.want {
				t.Errorf("BaseURL(%q) = %q, want %q", tc.region, got, tc.want)
			}
		})
	}
}
