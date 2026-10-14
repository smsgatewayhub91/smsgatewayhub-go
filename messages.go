package smsgatewayhub

import (
	"context"
	"strconv"
	"strings"
)

const messagesPath = "/cpaas/v1/messages.ashx"

// MessagesService is the CPaaS unified messages API (one call, many channels with fallback).
// It uses the CPaaS key.
type MessagesService struct {
	c *Client
}

// SendOptions are optional settings for Messages.Send.
type SendOptions struct {
	// IdempotencyKey is sent as the Idempotency-Key header. When empty a random UUID is generated.
	// The same key is reused on every retry, so a retry never sends twice.
	IdempotencyKey string
}

// ListParams filters Messages.List. Empty fields are omitted.
type ListParams struct {
	Status string
	Ref    string
	Limit  int
}

// Send sends a message (POST /cpaas/v1/messages.ashx). The body is passed through as JSON:
//
//	map[string]interface{}{
//		"channels": []string{"whatsapp", "sms"}, "to": "9198...", "fallbackAfter": 300,
//		"whatsapp": map[string]interface{}{"template": "order_update", "language": "en"},
//		"sms":      map[string]interface{}{"text": "...", "sender": "SGHTST"},
//	}
//
// The result is the parsed JSON object (contains "id" "cp_..." and "status"). opts may be nil.
func (s *MessagesService) Send(ctx context.Context, body map[string]interface{}, opts *SendOptions) (map[string]interface{}, error) {
	c := s.c
	if err := c.requireCPaaSKey(); err != nil {
		return nil, err
	}
	if body == nil {
		return nil, configError("message body is required")
	}
	key := ""
	if opts != nil {
		key = strings.TrimSpace(opts.IdempotencyKey)
	}
	if key == "" {
		key = newUUID()
	}
	resp, err := c.do(ctx, apiRequest{
		method: "POST", path: messagesPath, body: body, hasBody: true,
		cpaas: true, mode: retryAlways, idempotencyKey: key,
	})
	if err != nil {
		return nil, err
	}
	return c.cpaasResult(resp)
}

// Get returns the status and event timeline of one message ("cp_...").
func (s *MessagesService) Get(ctx context.Context, id string) (map[string]interface{}, error) {
	c := s.c
	if err := c.requireCPaaSKey(); err != nil {
		return nil, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, configError("id is required")
	}
	q := (&query{}).add("id", id)
	resp, err := c.do(ctx, apiRequest{method: "GET", path: messagesPath + "?" + q.String(), cpaas: true, mode: retryAlways})
	if err != nil {
		return nil, err
	}
	return c.cpaasResult(resp)
}

// List lists messages. params may be nil.
func (s *MessagesService) List(ctx context.Context, params *ListParams) (map[string]interface{}, error) {
	c := s.c
	if err := c.requireCPaaSKey(); err != nil {
		return nil, err
	}
	q := (&query{}).add("action", "list")
	if params != nil {
		q.addIf("status", strings.TrimSpace(params.Status))
		q.addIf("ref", strings.TrimSpace(params.Ref))
		if params.Limit > 0 {
			q.add("limit", strconv.Itoa(params.Limit))
		}
	}
	resp, err := c.do(ctx, apiRequest{method: "GET", path: messagesPath + "?" + q.String(), cpaas: true, mode: retryAlways})
	if err != nil {
		return nil, err
	}
	return c.cpaasResult(resp)
}
