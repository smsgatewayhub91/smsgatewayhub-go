// Command email_verify checks one address and starts a batch. Needs SGH_CPAAS_KEY.
package main

import (
	"context"
	"fmt"
	"log"

	smsgatewayhub "github.com/smsgatewayhub91/smsgatewayhub-go"
)

func main() {
	client := smsgatewayhub.NewClient()
	ctx := context.Background()

	one, err := client.EmailVerify.Check(ctx, "someone@example.com")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(one["result"], one["reason"], "suggestion:", one["suggestion"])

	batch, err := client.EmailVerify.Batch(ctx, []string{"a@example.com", "b@example.org", "c@example.net"}, "October leads")
	if err != nil {
		log.Fatal(err)
	}
	batchID, _ := batch["batchId"].(string)
	fmt.Println("batch", batchID)

	status, err := client.EmailVerify.BatchStatus(ctx, batchID, &smsgatewayhub.BatchStatusParams{Results: true, Page: 1, PageSize: 100})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(status)
}
