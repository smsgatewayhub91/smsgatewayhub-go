// Command send_sms sends a classic SMS. Needs SGH_API_KEY.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	smsgatewayhub "github.com/smsgatewayhub91/smsgatewayhub-go"
)

func main() {
	client := smsgatewayhub.NewClient() // reads SGH_API_KEY / SGH_BASE_URL

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := client.SMS.Send(ctx, smsgatewayhub.SendSMSRequest{
		SenderID:      "SGHTST",
		To:            []string{"919812345678", "919812345679"},
		Text:          "Dear customer, your order 1234 is shipped.",
		Channel:       smsgatewayhub.ChannelTransactional,
		EntityID:      "1101XXXXXXXXXXXXXXX",
		DLTTemplateID: "1207XXXXXXXXXXXXXXX",
	})
	if err != nil {
		var e *smsgatewayhub.Error
		if errors.As(err, &e) {
			log.Fatalf("send failed: status=%d code=%s message=%s", e.Status, e.Code, e.Message)
		}
		log.Fatal(err)
	}
	fmt.Println("job", res.JobID)
	for _, m := range res.Messages {
		fmt.Println(m.Number, "->", m.MessageID)
	}
}
