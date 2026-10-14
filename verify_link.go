package smsgatewayhub

import (
	"context"
	"strings"
)

const verifyLinkPath = "/cpaas/v1/verify-link.ashx"

// VerifyLinkService is OneTap Verify: passwordless one-time verification links sent over
// WhatsApp, SMS or email. It uses the CPaaS key.
//
// POST calls here are never repeated once the request reached the server; only connection
// failures are retried.
type VerifyLinkService struct {
	c *Client
}

// Create creates and sends a OneTap Verify link (POST /cpaas/v1/verify-link.ashx).
// Body keys: to, toEmail, channels, fallbackAfter, expiry, redirectUrl, state, ref, appName,
// sms, whatsapp, email. "{{link}}" inside channel objects is replaced server-side by the link.
func (s *VerifyLinkService) Create(ctx context.Context, body map[string]interface{}) (map[string]interface{}, error) {
	c := s.c
	if err := c.requireCPaaSKey(); err != nil {
		return nil, err
	}
	if body == nil {
		return nil, configError("body is required")
	}
	resp, err := c.do(ctx, apiRequest{method: "POST", path: verifyLinkPath, body: body, hasBody: true,
		cpaas: true, mode: retryConnectOnly})
	if err != nil {
		return nil, err
	}
	return c.cpaasResult(resp)
}

// Verify verifies the token from the vl_token query parameter on your redirectUrl (valid once).
// See GetVerifyToken.
func (s *VerifyLinkService) Verify(ctx context.Context, token string) (map[string]interface{}, error) {
	c := s.c
	if err := c.requireCPaaSKey(); err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, configError("token is required")
	}
	resp, err := c.do(ctx, apiRequest{method: "POST", path: verifyLinkPath + "?action=verify",
		body: map[string]interface{}{"token": token}, hasBody: true, cpaas: true, mode: retryConnectOnly})
	if err != nil {
		return nil, err
	}
	return c.cpaasResult(resp)
}

// Get returns the status of a link ("vl_..."): created, sent, opened, confirmed, verified,
// expired, failed or cancelled.
func (s *VerifyLinkService) Get(ctx context.Context, id string) (map[string]interface{}, error) {
	c := s.c
	if err := c.requireCPaaSKey(); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, configError("id is required")
	}
	q := (&query{}).add("id", id)
	resp, err := c.do(ctx, apiRequest{method: "GET", path: verifyLinkPath + "?" + q.String(), cpaas: true, mode: retryAlways})
	if err != nil {
		return nil, err
	}
	return c.cpaasResult(resp)
}

// Cancel cancels a link that has not been used yet.
func (s *VerifyLinkService) Cancel(ctx context.Context, id string) (map[string]interface{}, error) {
	c := s.c
	if err := c.requireCPaaSKey(); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, configError("id is required")
	}
	resp, err := c.do(ctx, apiRequest{method: "POST", path: verifyLinkPath + "?action=cancel",
		body: map[string]interface{}{"id": id}, hasBody: true, cpaas: true, mode: retryConnectOnly})
	if err != nil {
		return nil, err
	}
	return c.cpaasResult(resp)
}
