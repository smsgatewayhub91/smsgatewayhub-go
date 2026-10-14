// Command balance prints the SMS balance. Needs SGH_API_KEY.
package main

import (
	"context"
	"fmt"
	"log"

	smsgatewayhub "github.com/smsgatewayhub91/smsgatewayhub-go"
)

func main() {
	client := smsgatewayhub.NewClient()
	b, err := client.SMS.Balance(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	promo, _ := b.Float("Promo")
	trans, _ := b.Float("Trans")
	fmt.Printf("Promotional: %v\nTransactional: %v\nAll: %v\n", promo, trans, b.Balances)
}
