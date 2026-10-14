# SMSGatewayHub Go SDK

Official Go SDK for [SMSGatewayHub](https://www.smsgatewayhub.com): classic SMS API, CPaaS messages with channel fallback (WhatsApp, SMS, RCS, Email), OneTap Verify and Email Verification.

- Go 1.16 or newer, **standard library only** (net/http + encoding/json).
- Every call takes a `context.Context`; the client is safe for concurrent use.
- **Server-side only.** Never ship your keys inside mobile apps, desktop apps or browsers.

## Install

```
go get github.com/smsgatewayhub91/smsgatewayhub-go@v1.0.0
```

Direct download: `https://www.smsgatewayhub.com/sdk/smsgatewayhub-go-1.0.0.zip` - unzip next to your project and add
`replace github.com/smsgatewayhub91/smsgatewayhub-go => ./smsgatewayhub-go-1.0.0` to your `go.mod`.

## Quick start

```go
import smsgatewayhub "github.com/smsgatewayhub91/smsgatewayhub-go"

client := smsgatewayhub.NewClient(
	smsgatewayhub.WithAPIKey(os.Getenv("SGH_API_KEY")),     // classic SMS API key - Panel > API
	smsgatewayhub.WithCPaaSKey(os.Getenv("SGH_CPAAS_KEY")), // CPaaS key sghcp_... - Panel > CPaaS > API
)

res, err := client.SMS.Send(ctx, smsgatewayhub.SendSMSRequest{
	SenderID: "SGHTST",
	To:       []string{"919812345678"},
	Text:     "Your OTP is 123456",
})
if err != nil {
	log.Fatal(err)
}
fmt.Println(res.JobID)
```

`NewClient()` without options (or any option you leave out) reads `SGH_API_KEY`, `SGH_CPAAS_KEY` and `SGH_BASE_URL`.

| Option | Default | Notes |
|---|---|---|
| `WithAPIKey(string)` | env `SGH_API_KEY` | Needed for `SMS` |
| `WithCPaaSKey(string)` | env `SGH_CPAAS_KEY` | Needed for `Messages`, `VerifyLink`, `EmailVerify` |
| `WithBaseURL(string)` | env `SGH_BASE_URL`, else `https://www.smsgatewayhub.com` | Trailing slash removed |
| `WithTimeout(time.Duration)` | 30 s | Per request |
| `WithMaxRetries(int)` | 2 | 0 disables retries |
| `WithHTTPClient(*http.Client)` | own client | Proxies, custom TLS |

A group used without its key returns an `*Error` with `Code == "config_error"` before any network call.

## Classic SMS

```go
res, err := client.SMS.Send(ctx, smsgatewayhub.SendSMSRequest{
	SenderID:      "SGHTST",
	To:            []string{"919812345678", "919812345679"}, // max 100
	Text:          "Dear customer, your order 1234 is shipped.",
	Channel:       smsgatewayhub.ChannelTransactional, // "1", "2", "OTP", "promotional", "transactional", "otp" (default "2")
	Route:         "1",                                // default "1"
	Unicode:       nil,                                // nil = auto (any character above 0x7F -> DCS 8); smsgatewayhub.Bool(true) forces it
	Flash:         false,
	ScheduleTime:  time.Date(2026, 10, 6, 20, 30, 0, 0, ist), // or ScheduleAt: "2026/10/06 08:30:00 PM"
	EntityID:      "1101XXXXXXXXXXXXXXX",
	DLTTemplateID: "1207XXXXXXXXXXXXXXX",
})
for _, m := range res.Messages {
	fmt.Println(m.Number, m.MessageID)
}

bal, err := client.SMS.Balance(ctx)      // bal.Balances["Promo"] (float64), bal.Float("Trans")
dr, err := client.SMS.Delivery(ctx, res.JobID)
for _, r := range dr.Reports {
	fmt.Println(r.MessageID, r.Status, r.DeliveredAt)
}
```

`ScheduleTime` is formatted `yyyy/MM/dd hh:mm:ss AM|PM` in its own location (`smsgatewayhub.FormatScheduleTime`); `ScheduleAt` is sent as-is.

A response with `ErrorCode` other than `000`/`0` returns an `*Error` (Status 200, Code = ErrorCode, Message = ErrorMessage).

## CPaaS messages with fallback

One call, several channels: the next channel is tried when the previous one is not delivered within `fallbackAfter` seconds.

```go
msg, err := client.Messages.Send(ctx, map[string]interface{}{
	"channels":      []string{"whatsapp", "sms"},
	"to":            "919812345678",
	"fallbackAfter": 300,
	"ref":           "order-1234",
	"callbackUrl":   "https://example.com/hooks/sgh",
	"whatsapp": map[string]interface{}{
		"template": "order_update", "language": "en", "variables": []string{"Asha", "1234"},
	},
	"sms": map[string]interface{}{"text": "Hi Asha, your order 1234 is shipped.", "sender": "SGHTST"},
}, nil) // or &smsgatewayhub.SendOptions{IdempotencyKey: "order-1234-v1"}

fmt.Println(msg["id"], msg["status"]) // "cp_..."

st, err := client.Messages.Get(ctx, msg["id"].(string))  // status + event timeline
list, err := client.Messages.List(ctx, &smsgatewayhub.ListParams{Status: "delivered", Limit: 50})
```

Other body keys: `toEmail`, `rcs` (`template`, `bot`, `variables`), `email` (`from`, `fromName`, `replyTo`, `subject`, `html`, `text`, `templateId`, `placeholders`, `category`). Results are the full decoded JSON as `map[string]interface{}`.

## OneTap Verify

Passwordless sign-in with a one-time link sent over WhatsApp, SMS or email. `{{link}}` in a channel object is replaced by the link.

```go
link, err := client.VerifyLink.Create(ctx, map[string]interface{}{
	"to":          "919812345678",
	"channels":    []string{"whatsapp", "sms"},
	"redirectUrl": "https://myshop.example.com/auth/onetap",
	"state":       "session-abc",
	"expiry":      600,
	"sms":         map[string]interface{}{"text": "Tap to sign in to MyShop: {{link}}"},
})

// In your redirect handler:
http.HandleFunc("/auth/onetap", func(w http.ResponseWriter, r *http.Request) {
	token := smsgatewayhub.GetVerifyToken(r.URL.String()) // "" when missing
	res, err := client.VerifyLink.Verify(r.Context(), token) // valid once
	// ...
})

client.VerifyLink.Get(ctx, "vl_...")    // created / sent / opened / confirmed / verified / expired / failed / cancelled
client.VerifyLink.Cancel(ctx, "vl_...")
```

## Email Verification

```go
r, err := client.EmailVerify.Check(ctx, "someone@example.com")
// r["result"]: valid | invalid | risky | unknown; also reason, suggestion, disposable, roleBased, freeProvider, hasMx, domain

b, err := client.EmailVerify.Batch(ctx, emails, "October leads") // max 1000 -> b["batchId"]
st, err := client.EmailVerify.BatchStatus(ctx, b["batchId"].(string),
	&smsgatewayhub.BatchStatusParams{Results: true, Page: 1, PageSize: 100, Filter: "invalid"})
```

## Webhooks

Delivery / status callbacks (`callbackUrl`) are plain JSON POSTs to your server:

```go
http.HandleFunc("/hooks/sgh", func(w http.ResponseWriter, r *http.Request) {
	var event map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	log.Printf("event: %v", event)
	w.WriteHeader(http.StatusOK)
})
```

## Errors and retries

Every failure is an `*smsgatewayhub.Error`:

| Field | Meaning |
|---|---|
| `Status` | HTTP status, `0` for network / timeout / config errors |
| `Code` | e.g. `bad_request`, `unauthorized`, `http_502`, classic `ErrorCode` such as `13`, `network_error`, `timeout`, `config_error` |
| `Message` | Human readable (never contains your keys) |
| `Channel` | Failing channel for CPaaS errors (`sms`, `whatsapp`, ...) |
| `Raw` | Response body text, clipped to 2000 characters |

```go
var e *smsgatewayhub.Error
if errors.As(err, &e) {
	log.Printf("status=%d code=%s channel=%s: %s", e.Status, e.Code, e.Channel, e.Message)
}
```

Retries (`WithMaxRetries`, default 2, backoff 0.5 s then 1 s) happen only on network errors, HTTP 429 (honours `Retry-After`, capped at 10 s) and 5xx:

- `SMS.Send` (classic SendSMS) has no idempotency, so it is retried **only** when the connection could not be opened or on HTTP 429 - never after a timeout, read error or 5xx, because that could send the SMS twice.
- Other GET requests (`Balance`, `Delivery`, CPaaS `Get`/`List`/`BatchStatus`) are retried.
- `Messages.Send` always sends an `Idempotency-Key` (a random UUID unless you pass one) and reuses it on every retry, so a retry never sends twice.
- `VerifyLink.Create/Verify/Cancel` and `EmailVerify.Check/Batch` are retried only when the connection could not be opened.
- A cancelled or expired `ctx` stops retries immediately.

## White-label base URL

Resellers and white-label partners point the SDK at their own domain:

```go
client := smsgatewayhub.NewClient(
	smsgatewayhub.WithBaseURL("https://sms.your-brand.com"),
	smsgatewayhub.WithAPIKey(os.Getenv("SGH_API_KEY")),
)
```

or set `SGH_BASE_URL=https://sms.your-brand.com`.

## Security

- Use the SDK on your server only. Keys inside mobile apps, desktop apps or web pages can be extracted.
- Keep keys in environment variables `SGH_API_KEY` / `SGH_CPAAS_KEY` (or your secret manager), never in source control.
- The SDK never logs keys and removes them from error messages and raw bodies.
- Rotate a key in the panel immediately if it leaks.

## Tests

```
go test ./...
```

Tests use an in-process `httptest` server (no real network calls). With `SGH_BASE_URL=http://127.0.0.1:8765` a smoke test also runs against the shared mock server in `sdk/mock`.

## Support

- Email: support@smsgatewayhub.com
- Developer hub: https://www.smsgatewayhub.com/api-developer-hub

Licence: MIT.
