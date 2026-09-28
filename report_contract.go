package main

import (
	"fmt"
	"regexp"
	"strings"
)

const SchemaVersion = "1.1.0"
const ParserVersion = "1.2.0"
const ExtractorVersion = "poppler-bbox-1.1.0"
const DefaultReportBaseURL = "https://files.brokercheck.finra.org/individual"

var positiveCRD = regexp.MustCompile(`^[0-9]+$`)

func normalizeCRD(s string) (string, error) {
	if !positiveCRD.MatchString(s) {
		return "", fmt.Errorf("invalid CRD %q", s)
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return "", fmt.Errorf("CRD must be positive")
	}
	return s, nil
}
func reportVersionKey() string { return SchemaVersion + "-" + ParserVersion + "-" + ExtractorVersion }

type HTTPStatusError struct {
	StatusCode int
	URL        string
}

func (e *HTTPStatusError) Error() string { return fmt.Sprintf("HTTP %d for %s", e.StatusCode, e.URL) }
