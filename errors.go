package smsgatewayhub

import (
	"strconv"
)

// Error is the single error type returned by the SDK.
//
// Use errors.As(err, &e) with e of type *smsgatewayhub.Error to inspect it.
type Error struct {
	// Status is the HTTP status code, 0 for network, timeout and configuration errors.
	Status int
	// Code is a machine readable code, for example "bad_request", "http_502",
	// a classic SMS API ErrorCode such as "13", "network_error", "timeout" or "config_error".
	Code string
	// Message is a human readable message. It never contains your keys.
	Message string
	// Channel is the failing channel reported by the CPaaS API ("sms", "whatsapp", ...), may be empty.
	Channel string
	// Raw is the response body text, clipped to 2000 characters, may be empty.
	Raw string
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return "smsgatewayhub: <nil>"
	}
	s := "smsgatewayhub: " + e.Code
	if e.Status != 0 {
		s += " (HTTP " + strconv.Itoa(e.Status) + ")"
	}
	if e.Channel != "" {
		s += " [" + e.Channel + "]"
	}
	if e.Message != "" {
		s += ": " + e.Message
	}
	return s
}

// IsConfigError reports whether the error was raised before any network call
// because of a missing key or invalid parameters.
func (e *Error) IsConfigError() bool {
	return e != nil && e.Code == "config_error"
}

func configError(msg string) *Error {
	return &Error{Status: 0, Code: "config_error", Message: msg}
}
