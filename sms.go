package smsgatewayhub

import (
	"context"
	"strconv"
	"strings"
	"time"
)

// Channel values accepted by SendSMSRequest.Channel.
const (
	ChannelPromotional   = "1"
	ChannelTransactional = "2"
	ChannelOTP           = "OTP"
)

// ScheduleLayout is the Go time layout used for scheduled SMS, e.g. "2026/10/06 08:30:00 PM".
const ScheduleLayout = "2006/01/02 03:04:05 PM"

// MaxNumbersPerRequest is the maximum number of recipients in one SMS.Send call.
const MaxNumbersPerRequest = 100

// SMSService is the classic SMS API. It uses the API key.
type SMSService struct {
	c *Client
}

// SendSMSRequest describes one classic SMS send.
type SendSMSRequest struct {
	// SenderID is the approved sender id (required).
	SenderID string
	// To is the list of numbers (required, max 100). Entries may also contain comma separated numbers.
	To []string
	// Text is the message text (required).
	Text string
	// Channel: "1", "2", "OTP", "promotional", "transactional" or "otp". Default "2".
	Channel string
	// Route defaults to "1".
	Route string
	// Unicode forces DCS 8 (true) or 0 (false). Nil = auto-detect (any character above 0x7F -> 8).
	Unicode *bool
	// Flash sends a flash SMS.
	Flash bool
	// ScheduleAt is passed as-is, e.g. "2026/10/06 08:30:00 PM". Takes precedence over ScheduleTime.
	ScheduleAt string
	// ScheduleTime is formatted with ScheduleLayout (in its own location) when ScheduleAt is empty.
	ScheduleTime time.Time
	// GroupID, EntityID and DLTTemplateID are sent only when not empty.
	GroupID       string
	EntityID      string
	DLTTemplateID string
}

// Bool returns a pointer to b, handy for SendSMSRequest.Unicode.
func Bool(b bool) *bool { return &b }

// SentMessage is one accepted number with its message id.
type SentMessage struct {
	Number    string
	MessageID string
}

// SendResult is the result of SMS.Send.
type SendResult struct {
	JobID    string
	Messages []SentMessage
	// Raw is the full parsed API response.
	Raw map[string]interface{}
}

// BalanceResult is the result of SMS.Balance.
type BalanceResult struct {
	// Balances maps names such as "Promo" and "Trans" to float64 values
	// (or the original string when a value is not numeric).
	Balances map[string]interface{}
	// Raw is the full parsed API response.
	Raw map[string]interface{}
}

// Float returns a numeric balance by name (0, false when missing or not numeric).
func (b *BalanceResult) Float(name string) (float64, bool) {
	if b == nil {
		return 0, false
	}
	f, ok := b.Balances[name].(float64)
	return f, ok
}

// DeliveryReport is the delivery status of one message.
type DeliveryReport struct {
	MessageID   string
	Status      string
	DeliveredAt string
}

// DeliveryResult is the result of SMS.Delivery.
type DeliveryResult struct {
	Reports []DeliveryReport
	// Raw is the full parsed API response.
	Raw map[string]interface{}
}

// FormatScheduleTime formats t as "yyyy/MM/dd hh:mm:ss AM|PM" for scheduled SMS.
func FormatScheduleTime(t time.Time) string {
	return t.Format(ScheduleLayout)
}

// Send sends an SMS (GET /api/mt/SendSMS).
func (s *SMSService) Send(ctx context.Context, req SendSMSRequest) (*SendResult, error) {
	c := s.c
	if err := c.requireAPIKey(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.SenderID) == "" {
		return nil, configError("SenderID is required")
	}
	if req.Text == "" {
		return nil, configError("Text is required")
	}
	numbers, err := joinNumbers(req.To)
	if err != nil {
		return nil, err
	}
	channel, err := normalizeChannel(req.Channel)
	if err != nil {
		return nil, err
	}
	route := strings.TrimSpace(req.Route)
	if route == "" {
		route = "1"
	}
	unicode := hasNonASCII(req.Text)
	if req.Unicode != nil {
		unicode = *req.Unicode
	}
	dcs := "0"
	if unicode {
		dcs = "8"
	}
	flash := "0"
	if req.Flash {
		flash = "1"
	}
	sched := req.ScheduleAt
	if sched == "" && !req.ScheduleTime.IsZero() {
		sched = FormatScheduleTime(req.ScheduleTime)
	}

	q := &query{}
	q.add("APIKey", c.apiKey).
		add("senderid", strings.TrimSpace(req.SenderID)).
		add("channel", channel).
		add("DCS", dcs).
		add("flashsms", flash).
		add("number", numbers).
		add("text", req.Text).
		add("route", route)
	q.addIf("schedtime", sched).
		addIf("groupid", strings.TrimSpace(req.GroupID)).
		addIf("EntityId", strings.TrimSpace(req.EntityID)).
		addIf("dlttemplateid", strings.TrimSpace(req.DLTTemplateID))

	// SendSMS is not idempotent: retry only connect failures and HTTP 429 (never timeouts / 5xx).
	resp, err := c.do(ctx, apiRequest{method: "GET", path: "/api/mt/SendSMS?" + q.String(), mode: retryConnectOr429})
	if err != nil {
		return nil, err
	}
	m, err := c.classicResult(resp)
	if err != nil {
		return nil, err
	}
	res := &SendResult{JobID: toString(m["JobId"]), Messages: []SentMessage{}, Raw: m}
	if list, ok := m["MessageData"].([]interface{}); ok {
		for _, it := range list {
			if mm, ok := it.(map[string]interface{}); ok {
				res.Messages = append(res.Messages, SentMessage{
					Number:    toString(mm["Number"]),
					MessageID: toString(mm["MessageId"]),
				})
			}
		}
	}
	return res, nil
}

// Balance returns the account balance (GET /api/mt/GetBalance).
func (s *SMSService) Balance(ctx context.Context) (*BalanceResult, error) {
	c := s.c
	if err := c.requireAPIKey(); err != nil {
		return nil, err
	}
	q := (&query{}).add("APIKey", c.apiKey)
	resp, err := c.do(ctx, apiRequest{method: "GET", path: "/api/mt/GetBalance?" + q.String(), mode: retryAlways})
	if err != nil {
		return nil, err
	}
	m, err := c.classicResult(resp)
	if err != nil {
		return nil, err
	}
	return &BalanceResult{Balances: parseBalance(m["Balance"]), Raw: m}, nil
}

// Delivery returns delivery reports for a job id (GET /api/mt/GetDelivery).
func (s *SMSService) Delivery(ctx context.Context, jobID string) (*DeliveryResult, error) {
	c := s.c
	if err := c.requireAPIKey(); err != nil {
		return nil, err
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return nil, configError("jobID is required")
	}
	q := (&query{}).add("APIKey", c.apiKey).add("jobid", jobID)
	resp, err := c.do(ctx, apiRequest{method: "GET", path: "/api/mt/GetDelivery?" + q.String(), mode: retryAlways})
	if err != nil {
		return nil, err
	}
	m, err := c.classicResult(resp)
	if err != nil {
		return nil, err
	}
	res := &DeliveryResult{Reports: []DeliveryReport{}, Raw: m}
	if list, ok := m["DeliveryReports"].([]interface{}); ok {
		for _, it := range list {
			if mm, ok := it.(map[string]interface{}); ok {
				res.Reports = append(res.Reports, DeliveryReport{
					MessageID:   toString(mm["MessageId"]),
					Status:      toString(mm["DeliveryStatus"]),
					DeliveredAt: toString(mm["DeliveryDate"]),
				})
			}
		}
	}
	return res, nil
}

// ---------------------------------------------------------------- helpers

func joinNumbers(to []string) (string, error) {
	var items []string
	for _, entry := range to {
		for _, p := range strings.Split(entry, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				items = append(items, p)
			}
		}
	}
	if len(items) == 0 {
		return "", configError("To is required")
	}
	if len(items) > MaxNumbersPerRequest {
		return "", configError("Max 100 numbers per request")
	}
	return strings.Join(items, ","), nil
}

func normalizeChannel(ch string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(ch)) {
	case "", "2", "transactional":
		return ChannelTransactional, nil
	case "1", "promotional":
		return ChannelPromotional, nil
	case "otp":
		return ChannelOTP, nil
	}
	return "", configError(`Channel must be "1", "2" or "OTP" (or "promotional", "transactional", "otp")`)
}

func hasNonASCII(s string) bool {
	for _, r := range s {
		if r > 0x7F {
			return true
		}
	}
	return false
}

func parseBalance(v interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	switch t := v.(type) {
	case string:
		for _, part := range strings.Split(t, "|") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			name, value := "Balance", part
			if idx := strings.Index(part, ":"); idx >= 0 {
				name = strings.TrimSpace(part[:idx])
				value = strings.TrimSpace(part[idx+1:])
			}
			out[name] = numberOrString(value)
		}
	case map[string]interface{}:
		for k, val := range t {
			if sv, ok := val.(string); ok {
				out[k] = numberOrString(sv)
			} else {
				out[k] = val
			}
		}
	case nil:
		// no balance field
	default:
		out["Balance"] = t
	}
	return out
}

func numberOrString(s string) interface{} {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}
