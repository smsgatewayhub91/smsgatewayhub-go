# Changelog

## 1.0.0 - 09-10-2026

First release of the official SMSGatewayHub Go SDK.

- `SMS`: `Send`, `Balance`, `Delivery` (classic SMS API, typed `SendSMSRequest`, `SendResult`, `BalanceResult`, `DeliveryResult`).
- `Messages`: `Send` (automatic Idempotency-Key, safe retries), `Get`, `List` (CPaaS unified API with channel fallback).
- `VerifyLink`: OneTap Verify `Create`, `Verify`, `Get`, `Cancel`.
- `EmailVerify`: `Check`, `Batch`, `BatchStatus`.
- Helpers `GetVerifyToken` / `ParseRedirect`, `FormatScheduleTime`, `Bool`.
- One error type `*Error` with Status, Code, Message, Channel, Raw.
- Retries on network errors, HTTP 429 (Retry-After, max 10 s) and 5xx; classic `SMS.Send` only on connect failure and 429 (never sends twice).
- Go 1.16+, standard library only, `context.Context` on every call.
