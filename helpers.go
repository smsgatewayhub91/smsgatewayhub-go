package smsgatewayhub

import (
	"net/url"
	"strings"
)

// GetVerifyToken returns the vl_token query value from a full OneTap Verify callback URL
// (your redirectUrl as the user arrived on it), or "" when there is none.
func GetVerifyToken(callbackURL string) string {
	u, err := url.Parse(strings.TrimSpace(callbackURL))
	if err != nil {
		return ""
	}
	return u.Query().Get("vl_token")
}

// ParseRedirect is an alias of GetVerifyToken.
func ParseRedirect(callbackURL string) string {
	return GetVerifyToken(callbackURL)
}
