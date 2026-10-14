package smsgatewayhub

import (
	"context"
	"strconv"
	"strings"
)

const emailVerifyPath = "/cpaas/v1/email-verify.ashx"

// MaxEmailsPerBatch is the maximum number of addresses in one EmailVerify.Batch call.
const MaxEmailsPerBatch = 1000

// EmailVerifyService is Email Verification. It uses the CPaaS key.
type EmailVerifyService struct {
	c *Client
}

// BatchStatusParams are optional parameters for EmailVerify.BatchStatus. Zero values are omitted.
type BatchStatusParams struct {
	// Results includes per-address results (results=1).
	Results  bool
	Page     int
	PageSize int
	Filter   string
}

// Check verifies one address (POST /cpaas/v1/email-verify.ashx). The result contains
// result (valid, invalid, risky, unknown), reason, suggestion, disposable, roleBased,
// freeProvider, hasMx and domain.
func (s *EmailVerifyService) Check(ctx context.Context, email string) (map[string]interface{}, error) {
	c := s.c
	if err := c.requireCPaaSKey(); err != nil {
		return nil, err
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, configError("email is required")
	}
	resp, err := c.do(ctx, apiRequest{method: "POST", path: emailVerifyPath,
		body: map[string]interface{}{"email": email}, hasBody: true, cpaas: true, mode: retryConnectOnly})
	if err != nil {
		return nil, err
	}
	return c.cpaasResult(resp)
}

// Batch starts a batch verification (max 1000 addresses). name is optional.
// The result contains "batchId".
func (s *EmailVerifyService) Batch(ctx context.Context, emails []string, name string) (map[string]interface{}, error) {
	c := s.c
	if err := c.requireCPaaSKey(); err != nil {
		return nil, err
	}
	if len(emails) == 0 {
		return nil, configError("emails are required")
	}
	if len(emails) > MaxEmailsPerBatch {
		return nil, configError("Max 1000 emails per batch")
	}
	body := map[string]interface{}{"emails": emails}
	if strings.TrimSpace(name) != "" {
		body["name"] = name
	}
	resp, err := c.do(ctx, apiRequest{method: "POST", path: emailVerifyPath, body: body, hasBody: true,
		cpaas: true, mode: retryConnectOnly})
	if err != nil {
		return nil, err
	}
	return c.cpaasResult(resp)
}

// BatchStatus returns batch progress, and per-address results when params.Results is true.
// params may be nil.
func (s *EmailVerifyService) BatchStatus(ctx context.Context, batchID string, params *BatchStatusParams) (map[string]interface{}, error) {
	c := s.c
	if err := c.requireCPaaSKey(); err != nil {
		return nil, err
	}
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return nil, configError("batchID is required")
	}
	q := (&query{}).add("batch", batchID)
	if params != nil {
		if params.Results {
			q.add("results", "1")
		}
		if params.Page > 0 {
			q.add("page", strconv.Itoa(params.Page))
		}
		if params.PageSize > 0 {
			q.add("pageSize", strconv.Itoa(params.PageSize))
		}
		q.addIf("filter", strings.TrimSpace(params.Filter))
	}
	resp, err := c.do(ctx, apiRequest{method: "GET", path: emailVerifyPath + "?" + q.String(), cpaas: true, mode: retryAlways})
	if err != nil {
		return nil, err
	}
	return c.cpaasResult(resp)
}
