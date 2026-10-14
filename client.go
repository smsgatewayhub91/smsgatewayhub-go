// Package smsgatewayhub is the official Go SDK for SMSGatewayHub: classic SMS API,
// CPaaS messages with channel fallback, OneTap Verify and Email Verification.
//
// Server-side use only: never ship your keys inside mobile apps, desktop apps or browsers.
//
//	client := smsgatewayhub.NewClient(
//		smsgatewayhub.WithAPIKey(os.Getenv("SGH_API_KEY")),
//		smsgatewayhub.WithCPaaSKey(os.Getenv("SGH_CPAAS_KEY")),
//	)
//	res, err := client.SMS.Send(ctx, smsgatewayhub.SendSMSRequest{
//		SenderID: "SGHTST", To: []string{"919812345678"}, Text: "Hello",
//	})
//
// The package uses only the Go standard library.
package smsgatewayhub

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// Version is the SDK version.
	Version = "1.0.0"
	// DefaultBaseURL is the default API host. Resellers / white-label partners use WithBaseURL.
	DefaultBaseURL = "https://www.smsgatewayhub.com"
	// DefaultTimeout is the default request timeout.
	DefaultTimeout = 30 * time.Second
	// DefaultMaxRetries is the default number of retries for retryable failures.
	DefaultMaxRetries = 2

	userAgent = "smsgatewayhub-go/" + Version
	rawLimit  = 2000

	apiKeyMissing = "apiKey is required for the SMS group. Find it in the panel under Panel > API " +
		"(or set the SGH_API_KEY environment variable)."
	cpaasKeyMissing = "cpaasKey (sghcp_...) is required for Messages, VerifyLink and EmailVerify. Find it in the panel under " +
		"Panel > CPaaS > API (or set the SGH_CPAAS_KEY environment variable)."
)

// Client is the SMSGatewayHub API client. It is safe for concurrent use.
type Client struct {
	apiKey     string
	cpaasKey   string
	baseURL    string
	timeout    time.Duration
	maxRetries int
	httpClient *http.Client

	// SMS is the classic SMS API (needs the API key).
	SMS *SMSService
	// Messages is the CPaaS unified messages API (needs the CPaaS key).
	Messages *MessagesService
	// VerifyLink is OneTap Verify (needs the CPaaS key).
	VerifyLink *VerifyLinkService
	// EmailVerify is Email Verification (needs the CPaaS key).
	EmailVerify *EmailVerifyService
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey sets the classic SMS API key (Panel > API). Default: env SGH_API_KEY.
func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

// WithCPaaSKey sets the CPaaS key "sghcp_..." (Panel > CPaaS > API). Default: env SGH_CPAAS_KEY.
func WithCPaaSKey(key string) Option {
	return func(c *Client) { c.cpaasKey = key }
}

// WithBaseURL sets the API host, e.g. https://sms.your-brand.com.
// Default: env SGH_BASE_URL, else https://www.smsgatewayhub.com.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = baseURL }
}

// WithTimeout sets the per-request timeout (default 30 s). Ignored when WithHTTPClient is used.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// WithMaxRetries sets the maximum number of retries (default 2, 0 disables retries).
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = n }
}

// WithHTTPClient uses your own *http.Client (proxies, custom TLS, ...). Its Timeout is used as-is.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// NewClient creates a client. Options that are not given fall back to the environment
// variables SGH_API_KEY, SGH_CPAAS_KEY and SGH_BASE_URL.
func NewClient(opts ...Option) *Client {
	c := &Client{timeout: DefaultTimeout, maxRetries: DefaultMaxRetries}
	for _, o := range opts {
		if o != nil {
			o(c)
		}
	}
	c.apiKey = pick(c.apiKey, "SGH_API_KEY")
	c.cpaasKey = pick(c.cpaasKey, "SGH_CPAAS_KEY")
	c.baseURL = pick(c.baseURL, "SGH_BASE_URL")
	if c.baseURL == "" {
		c.baseURL = DefaultBaseURL
	}
	c.baseURL = strings.TrimRight(c.baseURL, "/")
	if c.timeout <= 0 {
		c.timeout = DefaultTimeout
	}
	if c.maxRetries < 0 {
		c.maxRetries = 0
	}
	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: c.timeout}
	}
	c.SMS = &SMSService{c: c}
	c.Messages = &MessagesService{c: c}
	c.VerifyLink = &VerifyLinkService{c: c}
	c.EmailVerify = &EmailVerifyService{c: c}
	return c
}

// BaseURL returns the effective base URL (no trailing slash).
func (c *Client) BaseURL() string { return c.baseURL }

func pick(value, envName string) string {
	v := strings.TrimSpace(value)
	if v != "" {
		return v
	}
	return strings.TrimSpace(os.Getenv(envName))
}

func (c *Client) requireAPIKey() error {
	if c.apiKey == "" {
		return configError(apiKeyMissing)
	}
	return nil
}

func (c *Client) requireCPaaSKey() error {
	if c.cpaasKey == "" {
		return configError(cpaasKeyMissing)
	}
	return nil
}

// ---------------------------------------------------------------- HTTP

type retryMode int

const (
	// retryAlways: GET requests and CPaaS message sends with an Idempotency-Key.
	retryAlways retryMode = iota
	// retryConnectOnly: other POSTs, retried only when no connection was obtained.
	retryConnectOnly
	// retryConnectOr429: classic SendSMS (not idempotent), retried only when no connection was
	// obtained or on HTTP 429 - never on timeout, read errors or 5xx (could send the SMS twice).
	retryConnectOr429
)

func (m retryMode) retryStatus(status int) bool {
	switch m {
	case retryAlways:
		return status == http.StatusTooManyRequests || status >= 500
	case retryConnectOr429:
		return status == http.StatusTooManyRequests
	default:
		return false
	}
}

type apiRequest struct {
	method         string
	path           string // path plus query string
	body           interface{}
	hasBody        bool
	cpaas          bool
	mode           retryMode
	idempotencyKey string
}

type apiResponse struct {
	status int
	body   []byte
}

func (c *Client) do(ctx context.Context, r apiRequest) (*apiResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var payload []byte
	if r.hasBody {
		b, err := json.Marshal(r.body)
		if err != nil {
			return nil, configError("could not encode request body as JSON: " + err.Error())
		}
		payload = b
	}
	fullURL := c.baseURL + r.path

	for attempt := 0; ; attempt++ {
		var bodyReader io.Reader
		if payload != nil {
			bodyReader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, r.method, fullURL, bodyReader)
		if err != nil {
			return nil, configError("invalid request URL (check the base URL): " + c.redact(err.Error()))
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json; charset=utf-8")
		}
		if r.cpaas {
			req.Header.Set("Authorization", "Bearer "+c.cpaasKey)
		}
		if r.idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", r.idempotencyKey)
		}
		if r.mode == retryConnectOr429 {
			// Fresh connection: stops net/http from transparently replaying this GET on a
			// stale keep-alive connection (which could send the SMS twice).
			req.Close = true
		}

		var connected int32
		trace := &httptrace.ClientTrace{
			GotConn: func(httptrace.GotConnInfo) { atomic.StoreInt32(&connected, 1) },
		}
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

		resp, err := c.httpClient.Do(req)
		if err != nil {
			sent := atomic.LoadInt32(&connected) == 1
			if ctx.Err() == nil && attempt < c.maxRetries && (r.mode == retryAlways || !sent) {
				if werr := sleepCtx(ctx, backoff(attempt)); werr != nil {
					return nil, c.networkError(werr)
				}
				continue
			}
			return nil, c.networkError(err)
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			if ctx.Err() == nil && attempt < c.maxRetries && r.mode == retryAlways {
				if werr := sleepCtx(ctx, backoff(attempt)); werr != nil {
					return nil, c.networkError(werr)
				}
				continue
			}
			return nil, c.networkError(readErr)
		}
		if r.mode.retryStatus(resp.StatusCode) && attempt < c.maxRetries {
			wait := backoff(attempt)
			if resp.StatusCode == http.StatusTooManyRequests {
				if d, ok := parseRetryAfter(resp.Header.Get("Retry-After")); ok {
					wait = d
				}
			}
			if werr := sleepCtx(ctx, wait); werr != nil {
				return nil, c.networkError(werr)
			}
			continue
		}
		return &apiResponse{status: resp.StatusCode, body: data}, nil
	}
}

func backoff(attempt int) time.Duration {
	if attempt > 3 {
		attempt = 3
	}
	d := 500 * time.Millisecond << uint(attempt)
	if d > 4*time.Second {
		d = 4 * time.Second
	}
	return d
}

func parseRetryAfter(v string) (time.Duration, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0, false
	}
	if n > 10 {
		n = 10
	}
	return time.Duration(n) * time.Second, true
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *Client) networkError(err error) *Error {
	code := "network_error"
	msg := err.Error()
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		msg = ue.Err.Error()
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		code = "timeout"
	}
	return &Error{Status: 0, Code: code, Message: c.redact("request failed: " + msg)}
}

// ---------------------------------------------------------------- response mapping

func (c *Client) cpaasResult(resp *apiResponse) (map[string]interface{}, error) {
	if resp.status < 200 || resp.status >= 300 {
		return nil, c.httpError(resp)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(stripBOM(resp.body), &m); err != nil || m == nil {
		return nil, &Error{Status: resp.status, Code: "invalid_response",
			Message: "response body is not a JSON object", Raw: c.clip(resp.body)}
	}
	return m, nil
}

func (c *Client) classicResult(resp *apiResponse) (map[string]interface{}, error) {
	if resp.status < 200 || resp.status >= 300 {
		return nil, c.httpError(resp)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(stripBOM(resp.body), &m); err != nil || m == nil {
		return nil, &Error{Status: resp.status, Code: "invalid_response",
			Message: "response body is not a JSON object", Raw: c.clip(resp.body)}
	}
	ec := toString(m["ErrorCode"])
	if ec != "" && ec != "000" && ec != "0" {
		msg := toString(m["ErrorMessage"])
		if msg == "" {
			msg = "SMS API error " + ec
		}
		return nil, &Error{Status: resp.status, Code: ec, Message: c.redact(msg), Raw: c.clip(resp.body)}
	}
	return m, nil
}

func (c *Client) httpError(resp *apiResponse) *Error {
	e := &Error{
		Status:  resp.status,
		Code:    "http_" + strconv.Itoa(resp.status),
		Message: "HTTP " + strconv.Itoa(resp.status),
		Raw:     c.clip(resp.body),
	}
	var m map[string]interface{}
	if json.Unmarshal(stripBOM(resp.body), &m) == nil && m != nil {
		switch ev := m["error"].(type) {
		case map[string]interface{}:
			if s := toString(ev["code"]); s != "" {
				e.Code = s
			}
			if s := toString(ev["message"]); s != "" {
				e.Message = c.redact(s)
			}
			e.Channel = toString(ev["channel"])
		case string:
			if ev != "" {
				e.Message = c.redact(ev)
			}
		default:
			if s := toString(m["ErrorMessage"]); s != "" {
				e.Message = c.redact(s)
			}
		}
	}
	return e
}

// ---------------------------------------------------------------- helpers

func stripBOM(b []byte) []byte {
	return bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
}

func (c *Client) clip(b []byte) string {
	s := c.redact(string(b))
	r := []rune(s)
	if len(r) > rawLimit {
		s = string(r[:rawLimit])
	}
	return s
}

// redact removes the keys (plain and URL-encoded) from text that may end up in an error.
func (c *Client) redact(s string) string {
	for _, k := range []string{c.apiKey, c.cpaasKey} {
		if k == "" {
			continue
		}
		s = strings.ReplaceAll(s, k, "***")
		if enc := url.QueryEscape(k); enc != k {
			s = strings.ReplaceAll(s, enc, "***")
		}
	}
	return s
}

// toString converts a decoded JSON scalar to a string ("" for nil).
func toString(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

// query is an ordered query-string builder; keys and values are URL-encoded (UTF-8).
type query struct {
	parts []string
}

func (q *query) add(k, v string) *query {
	q.parts = append(q.parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
	return q
}

func (q *query) addIf(k, v string) *query {
	if v != "" {
		q.add(k, v)
	}
	return q
}

func (q *query) String() string {
	return strings.Join(q.parts, "&")
}

func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("sgh-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
