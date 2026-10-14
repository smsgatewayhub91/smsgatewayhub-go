// Command onetap_verify_flow shows OneTap Verify, passwordless sign-in with a one-time link:
//
//  1. your server creates the link (WhatsApp first, SMS fallback);
//  2. the user taps it and lands on your redirectUrl with ?vl_token=...;
//  3. your server verifies the token (valid once) and signs the user in.
//
// Needs SGH_CPAAS_KEY. Run the HTTP handler part with: go run ./examples/onetap_verify_flow serve
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	smsgatewayhub "github.com/smsgatewayhub91/smsgatewayhub-go"
)

func main() {
	client := smsgatewayhub.NewClient()

	if len(os.Args) > 1 && os.Args[1] == "serve" {
		// Step 2/3: redirect handler on https://myshop.example.com/auth/onetap
		http.HandleFunc("/auth/onetap", func(w http.ResponseWriter, r *http.Request) {
			token := smsgatewayhub.GetVerifyToken(r.URL.String())
			if token == "" {
				http.Error(w, "missing token", http.StatusBadRequest)
				return
			}
			res, err := client.VerifyLink.Verify(r.Context(), token)
			if err != nil {
				http.Error(w, "verification failed", http.StatusUnauthorized)
				return
			}
			fmt.Fprintf(w, "verified: %v", res)
		})
		log.Fatal(http.ListenAndServe(":8080", nil))
	}

	// Step 1: create and send the link
	link, err := client.VerifyLink.Create(context.Background(), map[string]interface{}{
		"to":            "919812345678",
		"channels":      []string{"whatsapp", "sms"},
		"fallbackAfter": 60,
		"expiry":        600,
		"redirectUrl":   "https://myshop.example.com/auth/onetap",
		"state":         "session-abc",
		"appName":       "MyShop",
		"sms": map[string]interface{}{
			"text":   "Tap to sign in to MyShop: {{link}}",
			"sender": "SGHTST",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("created", link["id"], "status", link["status"])
}
