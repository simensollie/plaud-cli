package api

import (
	"errors"
	"fmt"
)

// Region identifies which Plaud backend a user account is bound to. Picking
// the wrong region returns 401 with no useful error, so the user picks at
// login time.
type Region string

const (
	RegionUS Region = "us"
	RegionEU Region = "eu"
	RegionJP Region = "jp"
)

// ErrUnknownRegion is returned when BaseURL is called with a Region that does
// not correspond to a known Plaud backend.
var ErrUnknownRegion = errors.New("unknown region")

var regionBaseURL = map[Region]string{
	RegionUS: "https://api.plaud.ai",
	RegionEU: "https://api-euc1.plaud.ai",
	RegionJP: "https://api-jp.plaud.ai",
}

// BaseURL returns the API host for the given region, or ErrUnknownRegion if
// the region is not recognized.
func BaseURL(r Region) (string, error) {
	url, ok := regionBaseURL[r]
	if !ok {
		return "", fmt.Errorf("region %q: %w", r, ErrUnknownRegion)
	}
	return url, nil
}
