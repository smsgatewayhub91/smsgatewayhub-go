// Command delivery prints delivery reports for a job id. Needs SGH_API_KEY.
//
//	go run ./examples/delivery 20047
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	smsgatewayhub "github.com/smsgatewayhub91/smsgatewayhub-go"
)

func main() {
	jobID := "20047"
	if len(os.Args) > 1 {
		jobID = os.Args[1]
	}
	client := smsgatewayhub.NewClient()
	d, err := client.SMS.Delivery(context.Background(), jobID)
	if err != nil {
		log.Fatal(err)
	}
	for _, r := range d.Reports {
		fmt.Println(r.MessageID, r.Status, r.DeliveredAt)
	}
}
