// Package verexa checks every prompt and model response against your Verexa
// policy before it reaches the model or the user.
//
//	guard := verexa.New(verexa.Config{})
//	httpClient := &http.Client{Transport: guard.Transport(nil)}
//
// Every check fails open by default: if the service is unreachable the
// verdict is "allow" with Degraded set, and a circuit breaker stops the
// client from hammering it. Set FailMode to FailClosed to block instead.
package verexa
