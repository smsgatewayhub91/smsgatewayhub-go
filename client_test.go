package smsgatewayhub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Self-contained tests: an in-process httptest server plays the API. No real network calls.

const (
	testAPIKey   = "test-api-key-123"
	testCPaaSKey = "sghcp_testkey456"
)

type recorded struct {
	Method string
	Path   string
	Query  url.Values
	Body   string
	Header http.Header
}

type mockServer struct {
	*httptest.Server
	mu      sync.Mutex
	reqs    []recorded
	seen429 map[string]bool
	keys429 []string
}

func newMock(t *testing.T) *mockServer {
	t.Helper()
	m := &mockServer{seen429: map[string]bool{}}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.Server.Close)
	return m
}

func (m *mockServer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.reqs)
}

func (m *mockServer) last() recorded {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.reqs) == 0 {
		return recorded{}
	}
	return m.reqs[len(m.reqs)-1]
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func (m *mockServer) handle(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	body := string(b)
	q := r.URL.Query()
	m.mu.Lock()
	m.reqs = append(m.reqs, recorded{Method: r.Method, Path: r.URL.Path, Query: q, Body: body, Header: r.Header.Clone()})
	m.mu.Unlock()

	switch r.URL.Path {
	case "/api/mt/SendSMS":
		if q.Get("text") == "e500" {
			writeJSON(w, 500, `{"error":{"code":"server_error","message":"boom"}}`)
			return
		}
		if q.Get("text") == "r429" {
			m.mu.Lock()
			first := !m.seen429["sms"]
			m.seen429["sms"] = true
			m.mu.Unlock()
			if first {
				w.Header().Set("Retry-After", "0")
				writeJSON(w, 429, `{"ErrorCode":"429","ErrorMessage":"Too many requests"}`)
				return
			}
		}
		if q.Get("text") == "fail" {
			writeJSON(w, 200, `{"ErrorCode":"13","ErrorMessage":"Invalid sender","JobId":null,"MessageData":null,"echo":"`+q.Get("APIKey")+`"}`)
			return
		}
		writeJSON(w, 200, `{"ErrorCode":"000","ErrorMessage":"Success","JobId":"20047","MessageData":[{"Number":"919812345678","MessageId":"m1"}]}`)
	case "/api/mt/GetBalance":
		writeJSON(w, 200, `{"ErrorCode":"0","ErrorMessage":"Success","Balance":"Promo:9988|Trans:0|Intl:12.5"}`)
	case "/api/mt/GetDelivery":
		writeJSON(w, 200, `{"ErrorCode":"000","ErrorMessage":"Success","DeliveryReports":[{"MessageId":"m1","DeliveryStatus":"DELIVRD","DeliveryDate":"2026-10-09 10:00:00"}]}`)
	case "/cpaas/v1/messages.ashx":
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer sghcp_") {
			writeJSON(w, 401, `{"error":{"code":"unauthorized","message":"Invalid CPaaS key"}}`)
			return
		}
		if r.Method == http.MethodPost {
			key := r.Header.Get("Idempotency-Key")
			if strings.Contains(body, `"ref":"retry429"`) {
				m.mu.Lock()
				m.keys429 = append(m.keys429, key)
				first := !m.seen429[key]
				m.seen429[key] = true
				m.mu.Unlock()
				if first {
					w.Header().Set("Retry-After", "0")
					writeJSON(w, 429, `{"error":{"code":"rate_limited","message":"slow down"}}`)
					return
				}
			}
			if strings.Contains(body, `"ref":"bad"`) {
				writeJSON(w, 400, `{"error":{"code":"bad_request","message":"to is required","channel":"sms"}}`)
				return
			}
			writeJSON(w, 200, `{"id":"cp_test","status":"queued","idempotencyKey":"`+key+`"}`)
			return
		}
		switch {
		case q.Get("action") == "list":
			writeJSON(w, 200, `{"data":[]}`)
		case q.Get("id") == "down":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(502)
			_, _ = io.WriteString(w, "Bad Gateway")
		default:
			writeJSON(w, 200, `{"id":"`+q.Get("id")+`","status":"delivered","events":[]}`)
		}
	case "/cpaas/v1/verify-link.ashx":
		switch {
		case r.Method == http.MethodPost && q.Get("action") == "verify":
			writeJSON(w, 200, `{"id":"vl_1","status":"verified","verified":true}`)
		case r.Method == http.MethodPost && q.Get("action") == "cancel":
			writeJSON(w, 200, `{"id":"vl_1","status":"cancelled"}`)
		case r.Method == http.MethodPost && strings.Contains(body, `"ref":"boom"`):
			writeJSON(w, 500, `{"error":{"code":"server_error","message":"boom"}}`)
		case r.Method == http.MethodPost:
			writeJSON(w, 200, `{"id":"vl_1","status":"sent"}`)
		default:
			writeJSON(w, 200, `{"id":"`+q.Get("id")+`","status":"verified"}`)
		}
	case "/cpaas/v1/email-verify.ashx":
		switch {
		case r.Method == http.MethodPost && strings.Contains(body, `"emails"`):
			writeJSON(w, 200, `{"batchId":"ev_1"}`)
		case r.Method == http.MethodPost:
			writeJSON(w, 200, `{"result":"valid","reason":"accepted","hasMx":true}`)
		default:
			writeJSON(w, 200, `{"batchId":"`+q.Get("batch")+`","status":"completed"}`)
		}
	default:
		writeJSON(w, 404, `{"error":{"code":"not_found","message":"Not found"}}`)
	}
}

func newTestClient(m *mockServer, extra ...Option) *Client {
	opts := []Option{
		WithAPIKey(testAPIKey),
		WithCPaaSKey(testCPaaSKey),
		WithBaseURL(m.URL + "/"),
		WithTimeout(5 * time.Second),
	}
	opts = append(opts, extra...)
	return NewClient(opts...)
}

func asError(t *testing.T, err error) *Error {
	t.Helper()
	if err == nil {
		t.Fatalf("expected an error, got nil")
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	return e
}

func unsetEnv(t *testing.T, names ...string) {
	t.Helper()
	for _, n := range names {
		name := n
		old, had := os.LookupEnv(name)
		_ = os.Unsetenv(name)
		t.Cleanup(func() {
			if had {
				_ = os.Setenv(name, old)
			}
		})
	}
}

func decodeBody(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("request body is not JSON: %v (%q)", err, s)
	}
	return m
}

func eq(t *testing.T, what string, want, got interface{}) {
	t.Helper()
	if want != got {
		t.Errorf("%s: want %v (%T), got %v (%T)", what, want, want, got, got)
	}
}

// ---------------------------------------------------------------- tests

func TestBaseURLTrimmed(t *testing.T) {
	c := NewClient(WithBaseURL("https://sms.example.com///"), WithAPIKey("k"))
	eq(t, "base", "https://sms.example.com", c.BaseURL())
}

func TestSMSSend(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	text := "Namaste नमस्ते & more"
	res, err := c.SMS.Send(context.Background(), SendSMSRequest{
		SenderID:      "SGHTST",
		To:            []string{"919812345678", "919812345679"},
		Text:          text,
		Channel:       "otp",
		EntityID:      "1101",
		DLTTemplateID: "1207",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	eq(t, "jobId", "20047", res.JobID)
	if len(res.Messages) != 1 || res.Messages[0].MessageID != "m1" || res.Messages[0].Number != "919812345678" {
		t.Errorf("messages: %+v", res.Messages)
	}
	rec := m.last()
	eq(t, "path", "/api/mt/SendSMS", rec.Path)
	eq(t, "method", "GET", rec.Method)
	eq(t, "APIKey", testAPIKey, rec.Query.Get("APIKey"))
	eq(t, "senderid", "SGHTST", rec.Query.Get("senderid"))
	eq(t, "channel", "OTP", rec.Query.Get("channel"))
	eq(t, "DCS", "8", rec.Query.Get("DCS"))
	eq(t, "flashsms", "0", rec.Query.Get("flashsms"))
	eq(t, "number", "919812345678,919812345679", rec.Query.Get("number"))
	eq(t, "text", text, rec.Query.Get("text"))
	eq(t, "route", "1", rec.Query.Get("route"))
	eq(t, "EntityId", "1101", rec.Query.Get("EntityId"))
	eq(t, "dlttemplateid", "1207", rec.Query.Get("dlttemplateid"))
	if _, ok := rec.Query["schedtime"]; ok {
		t.Errorf("schedtime should be omitted")
	}
	if _, ok := rec.Query["groupid"]; ok {
		t.Errorf("groupid should be omitted")
	}
	eq(t, "User-Agent", "smsgatewayhub-go/1.0.0", rec.Header.Get("User-Agent"))
	eq(t, "Accept", "application/json", rec.Header.Get("Accept"))
}

func TestSMSSendOptions(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	_, err := c.SMS.Send(context.Background(), SendSMSRequest{
		SenderID:     "SGHTST",
		To:           []string{"919812345678"},
		Text:         "Hello",
		Channel:      "promotional",
		Flash:        true,
		ScheduleTime: time.Date(2026, 10, 6, 20, 30, 0, 0, time.UTC),
		GroupID:      "g1",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	q := m.last().Query
	eq(t, "DCS", "0", q.Get("DCS"))
	eq(t, "channel", "1", q.Get("channel"))
	eq(t, "flashsms", "1", q.Get("flashsms"))
	eq(t, "schedtime", "2026/10/06 08:30:00 PM", q.Get("schedtime"))
	eq(t, "groupid", "g1", q.Get("groupid"))

	_, err = c.SMS.Send(context.Background(), SendSMSRequest{
		SenderID: "SGHTST", To: []string{"919812345678"}, Text: "Hello",
		Unicode: Bool(true), ScheduleAt: "2026/10/07 09:00:00 AM",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	q = m.last().Query
	eq(t, "DCS forced", "8", q.Get("DCS"))
	eq(t, "schedtime as-is", "2026/10/07 09:00:00 AM", q.Get("schedtime"))
	eq(t, "default channel", "2", q.Get("channel"))
}

func TestSMSErrorCode(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	_, err := c.SMS.Send(context.Background(), SendSMSRequest{SenderID: "SGHTST", To: []string{"919812345678"}, Text: "fail"})
	e := asError(t, err)
	eq(t, "status", 200, e.Status)
	eq(t, "code", "13", e.Code)
	eq(t, "message", "Invalid sender", e.Message)
	if strings.Contains(e.Raw, testAPIKey) || strings.Contains(e.Error(), testAPIKey) {
		t.Errorf("key leaked into error: %q", e.Raw)
	}
}

func TestSMSSendNotRetriedOn5xx(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	_, err := c.SMS.Send(context.Background(), SendSMSRequest{SenderID: "SGHTST", To: []string{"919812345678"}, Text: "e500"})
	e := asError(t, err)
	eq(t, "status", 500, e.Status)
	eq(t, "attempts", 1, m.count())
}

func TestSMSSendRetriedOn429(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	res, err := c.SMS.Send(context.Background(), SendSMSRequest{SenderID: "SGHTST", To: []string{"919812345678"}, Text: "r429"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	eq(t, "jobId", "20047", res.JobID)
	eq(t, "attempts", 2, m.count())
}

func TestSMSValidation(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	many := make([]string, 101)
	for i := range many {
		many[i] = "91980000" + strconv.Itoa(1000+i)
	}
	before := m.count()
	_, err := c.SMS.Send(context.Background(), SendSMSRequest{SenderID: "SGHTST", To: many, Text: "Hello"})
	e := asError(t, err)
	eq(t, "code", "config_error", e.Code)
	eq(t, "message", "Max 100 numbers per request", e.Message)

	_, err = c.SMS.Send(context.Background(), SendSMSRequest{SenderID: "SGHTST", To: []string{"9198"}, Text: "x", Channel: "bogus"})
	eq(t, "bad channel", "config_error", asError(t, err).Code)

	_, err = c.SMS.Send(context.Background(), SendSMSRequest{SenderID: "SGHTST", Text: "x"})
	eq(t, "no numbers", "config_error", asError(t, err).Code)

	eq(t, "no request made", before, m.count())
}

func TestMissingKeys(t *testing.T) {
	unsetEnv(t, "SGH_API_KEY", "SGH_CPAAS_KEY")
	m := newMock(t)
	c := NewClient(WithBaseURL(m.URL))
	_, err := c.SMS.Balance(context.Background())
	e := asError(t, err)
	eq(t, "code", "config_error", e.Code)
	eq(t, "status", 0, e.Status)
	if !strings.Contains(e.Message, "Panel > API") {
		t.Errorf("message should name the panel page: %q", e.Message)
	}
	_, err = c.EmailVerify.Check(context.Background(), "a@b.com")
	e = asError(t, err)
	eq(t, "code", "config_error", e.Code)
	if !strings.Contains(e.Message, "Panel > CPaaS > API") {
		t.Errorf("message should name the panel page: %q", e.Message)
	}
	eq(t, "no request made", 0, m.count())
}

func TestEnvFallback(t *testing.T) {
	m := newMock(t)
	unsetEnv(t, "SGH_API_KEY", "SGH_CPAAS_KEY", "SGH_BASE_URL")
	_ = os.Setenv("SGH_API_KEY", "env-key-1")
	_ = os.Setenv("SGH_BASE_URL", m.URL)
	t.Cleanup(func() {
		_ = os.Unsetenv("SGH_API_KEY")
		_ = os.Unsetenv("SGH_BASE_URL")
	})
	c := NewClient()
	if _, err := c.SMS.Balance(context.Background()); err != nil {
		t.Fatalf("balance: %v", err)
	}
	eq(t, "APIKey from env", "env-key-1", m.last().Query.Get("APIKey"))
}

func TestBalance(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	b, err := c.SMS.Balance(context.Background())
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	eq(t, "Promo", 9988.0, b.Balances["Promo"])
	eq(t, "Trans", 0.0, b.Balances["Trans"])
	eq(t, "Intl", 12.5, b.Balances["Intl"])
	if f, ok := b.Float("Promo"); !ok || f != 9988 {
		t.Errorf("Float(Promo) = %v, %v", f, ok)
	}
}

func TestDelivery(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	d, err := c.SMS.Delivery(context.Background(), "20047")
	if err != nil {
		t.Fatalf("delivery: %v", err)
	}
	eq(t, "jobid", "20047", m.last().Query.Get("jobid"))
	if len(d.Reports) != 1 {
		t.Fatalf("reports: %+v", d.Reports)
	}
	eq(t, "messageId", "m1", d.Reports[0].MessageID)
	eq(t, "status", "DELIVRD", d.Reports[0].Status)
	eq(t, "deliveredAt", "2026-10-09 10:00:00", d.Reports[0].DeliveredAt)
}

func TestMessagesSendRetries429WithSameKey(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	body := map[string]interface{}{
		"channels": []string{"whatsapp", "sms"},
		"to":       "919812345678",
		"ref":      "retry429",
		"sms":      map[string]interface{}{"text": "Hi"},
	}
	res, err := c.Messages.Send(context.Background(), body, nil)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	eq(t, "id", "cp_test", res["id"])
	m.mu.Lock()
	keys := append([]string(nil), m.keys429...)
	m.mu.Unlock()
	if len(keys) != 2 || keys[0] != keys[1] || len(keys[0]) != 36 {
		t.Fatalf("expected two attempts with the same UUID key, got %q", keys)
	}
	eq(t, "echoed key", keys[0], res["idempotencyKey"])
	rec := m.last()
	eq(t, "auth", "Bearer "+testCPaaSKey, rec.Header.Get("Authorization"))
	eq(t, "content-type", "application/json; charset=utf-8", rec.Header.Get("Content-Type"))
	eq(t, "body passthrough", "retry429", decodeBody(t, rec.Body)["ref"])
}

func TestMessagesSendCallerKey(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	res, err := c.Messages.Send(context.Background(), map[string]interface{}{"to": "9198"}, &SendOptions{IdempotencyKey: "my-key-1"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	eq(t, "key", "my-key-1", res["idempotencyKey"])
	eq(t, "header", "my-key-1", m.last().Header.Get("Idempotency-Key"))
}

func TestMessagesErrorEnvelope(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	_, err := c.Messages.Send(context.Background(), map[string]interface{}{"ref": "bad"}, nil)
	e := asError(t, err)
	eq(t, "status", 400, e.Status)
	eq(t, "code", "bad_request", e.Code)
	eq(t, "message", "to is required", e.Message)
	eq(t, "channel", "sms", e.Channel)
	eq(t, "attempts", 1, m.count())
}

func TestMessagesGetAndList(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	res, err := c.Messages.Get(context.Background(), "cp_abc")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	eq(t, "id", "cp_abc", res["id"])
	eq(t, "query id", "cp_abc", m.last().Query.Get("id"))

	if _, err := c.Messages.List(context.Background(), &ListParams{Status: "delivered", Limit: 10}); err != nil {
		t.Fatalf("list: %v", err)
	}
	q := m.last().Query
	eq(t, "action", "list", q.Get("action"))
	eq(t, "status", "delivered", q.Get("status"))
	eq(t, "limit", "10", q.Get("limit"))
	if _, ok := q["ref"]; ok {
		t.Errorf("empty ref should be omitted")
	}
	if _, err := c.Messages.List(context.Background(), nil); err != nil {
		t.Fatalf("list nil: %v", err)
	}
}

func TestGetRetriedOn502ThenHTTPError(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m, WithMaxRetries(1))
	_, err := c.Messages.Get(context.Background(), "down")
	e := asError(t, err)
	eq(t, "status", 502, e.Status)
	eq(t, "code", "http_502", e.Code)
	eq(t, "raw", "Bad Gateway", e.Raw)
	eq(t, "attempts", 2, m.count())
}

func TestVerifyLinkCreateNotRetriedOn500(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	_, err := c.VerifyLink.Create(context.Background(), map[string]interface{}{"ref": "boom"})
	e := asError(t, err)
	eq(t, "status", 500, e.Status)
	eq(t, "code", "server_error", e.Code)
	eq(t, "attempts", 1, m.count())
}

func TestVerifyLinkFlow(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	ctx := context.Background()

	res, err := c.VerifyLink.Create(ctx, map[string]interface{}{
		"to":          "919812345678",
		"channels":    []string{"whatsapp", "sms"},
		"redirectUrl": "https://example.com/cb",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	eq(t, "create id", "vl_1", res["id"])
	eq(t, "create method", "POST", m.last().Method)

	if _, err := c.VerifyLink.Verify(ctx, "tok123"); err != nil {
		t.Fatalf("verify: %v", err)
	}
	rec := m.last()
	eq(t, "verify action", "verify", rec.Query.Get("action"))
	eq(t, "token", "tok123", decodeBody(t, rec.Body)["token"])

	g, err := c.VerifyLink.Get(ctx, "vl_1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	eq(t, "get status", "verified", g["status"])
	eq(t, "get id", "vl_1", m.last().Query.Get("id"))

	cn, err := c.VerifyLink.Cancel(ctx, "vl_1")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	eq(t, "cancel status", "cancelled", cn["status"])
	rec = m.last()
	eq(t, "cancel action", "cancel", rec.Query.Get("action"))
	eq(t, "cancel id", "vl_1", decodeBody(t, rec.Body)["id"])
}

func TestEmailVerify(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	ctx := context.Background()

	r, err := c.EmailVerify.Check(ctx, "a@b.com")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	eq(t, "result", "valid", r["result"])
	eq(t, "email body", "a@b.com", decodeBody(t, m.last().Body)["email"])

	b, err := c.EmailVerify.Batch(ctx, []string{"a@b.com", "c@d.com"}, "list1")
	if err != nil {
		t.Fatalf("batch: %v", err)
	}
	eq(t, "batchId", "ev_1", b["batchId"])
	sent := decodeBody(t, m.last().Body)
	eq(t, "name", "list1", sent["name"])
	if list, ok := sent["emails"].([]interface{}); !ok || len(list) != 2 {
		t.Errorf("emails: %v", sent["emails"])
	}

	if _, err := c.EmailVerify.BatchStatus(ctx, "ev_1", &BatchStatusParams{Results: true, Page: 2, PageSize: 50, Filter: "invalid"}); err != nil {
		t.Fatalf("batchStatus: %v", err)
	}
	q := m.last().Query
	eq(t, "batch", "ev_1", q.Get("batch"))
	eq(t, "results", "1", q.Get("results"))
	eq(t, "page", "2", q.Get("page"))
	eq(t, "pageSize", "50", q.Get("pageSize"))
	eq(t, "filter", "invalid", q.Get("filter"))

	before := m.count()
	many := make([]string, 1001)
	for i := range many {
		many[i] = "u" + strconv.Itoa(i) + "@x.com"
	}
	_, err = c.EmailVerify.Batch(ctx, many, "")
	eq(t, "max 1000", "config_error", asError(t, err).Code)
	eq(t, "no request made", before, m.count())
}

func TestNetworkErrorRedacted(t *testing.T) {
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	c := NewClient(WithAPIKey(testAPIKey), WithCPaaSKey(testCPaaSKey), WithBaseURL(deadURL),
		WithMaxRetries(0), WithTimeout(2*time.Second))
	_, err := c.SMS.Balance(context.Background())
	e := asError(t, err)
	eq(t, "status", 0, e.Status)
	if e.Code != "network_error" && e.Code != "timeout" {
		t.Errorf("code: %q", e.Code)
	}
	if strings.Contains(e.Error(), testAPIKey) {
		t.Errorf("key leaked: %q", e.Error())
	}
}

func TestContextCancelled(t *testing.T) {
	m := newMock(t)
	c := newTestClient(m)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.SMS.Balance(ctx)
	e := asError(t, err)
	eq(t, "status", 0, e.Status)
}

func TestGetVerifyToken(t *testing.T) {
	eq(t, "token", "abc 123", GetVerifyToken("https://x.com/cb?state=s&vl_token=abc%20123#frag"))
	eq(t, "alias", "t1", ParseRedirect("https://x.com/cb?vl_token=t1"))
	eq(t, "missing", "", GetVerifyToken("https://x.com/cb?state=s"))
	eq(t, "empty", "", GetVerifyToken(""))
}

func TestErrorString(t *testing.T) {
	e := &Error{Status: 400, Code: "bad_request", Message: "to is required", Channel: "sms"}
	eq(t, "Error()", "smsgatewayhub: bad_request (HTTP 400) [sms]: to is required", e.Error())
	var err error = e
	var target *Error
	if !errors.As(err, &target) || !(&Error{Code: "config_error"}).IsConfigError() {
		t.Errorf("errors.As / IsConfigError")
	}
}
