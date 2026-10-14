// Command whatsapp_with_sms_fallback sends a WhatsApp template and falls back to SMS when it is
// not delivered within 300 seconds. Needs SGH_CPAAS_KEY (sghcp_...).
package main

import (
	"context"
	"errors"
	"fmt"
	"log"

	smsgatewayhub "github.com/smsgatewayhub91/smsgatewayhub-go"
)

func main() {
	client := smsgatewayhub.NewClient()
	ctx := context.Background()

	msg, err := client.Messages.Send(ctx, map[string]interface{}{
		"channels":      []string{"whatsapp", "sms"},
		"to":            "919812345678",
		"fallbackAfter": 300,
		"ref":           "order-1234",
		"callbackUrl":   "https://example.com/hooks/sgh",
		"whatsapp": map[string]interface{}{
			"template":  "order_update",
			"language":  "en",
			"variables": []string{"Asha", "1234"},
		},
		"sms": map[string]interface{}{
			"text":       "Hi Asha, your order 1234 is shipped.",
			"sender":     "SGHTST",
			"templateId": "1207XXXXXXXXXXXXXXX",
			"entityId":   "1101XXXXXXXXXXXXXXX",
		},
	}, nil)
	if err != nil {
		var e *smsgatewayhub.Error
		if errors.As(err, &e) {
			log.Fatalf("failed: status=%d code=%s channel=%s message=%s", e.Status, e.Code, e.Channel, e.Message)
		}
		log.Fatal(err)
	}
	fmt.Println("queued", msg["id"], "status", msg["status"])

	id, _ := msg["id"].(string)
	status, err := client.Messages.Get(ctx, id)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("now:", status)
}
