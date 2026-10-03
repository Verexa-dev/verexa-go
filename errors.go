package verexa

import "strings"

// BlockedError is returned when a verdict blocks a turn. Callers going through
// an http.Client see it wrapped in a *url.Error; use errors.As to recover it.
type BlockedError struct {
	Phase    Phase
	Response *CheckResponse
}

func (e *BlockedError) Error() string {
	var flagged []string
	if e.Response != nil {
		for _, d := range e.Response.Detectors {
			if d.Action != ActionAllow {
				flagged = append(flagged, d.DetectorID)
			}
		}
	}
	reason := strings.Join(flagged, ", ")
	if reason == "" {
		reason = "policy action block"
	}
	return "guard blocked " + string(e.Phase) + ": " + reason
}
