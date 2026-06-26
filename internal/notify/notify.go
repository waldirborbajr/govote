// Package notify handles outbound notifications to voters and admins. For this
// PoC it builds WhatsApp deep links and logs end-of-poll result notifications.
package notify

import (
	"fmt"
	"log"
	"net/url"

	"github.com/waldirborbajr/govote/internal/models"
)

// BuildWhatsAppURL builds a wa.me deep link carrying the passcode message.
func BuildWhatsAppURL(phone, passcode string) string {
	text := fmt.Sprintf("Your voting system passcode is: %s\n\nDo not share this code with anyone.", passcode)
	encodedText := url.QueryEscape(text)
	return fmt.Sprintf("https://wa.me/%s?text=%s", phone, encodedText)
}

// SimulateNotification logs that a poll has ended together with its results.
func SimulateNotification(pollID int64, results []models.ResultAnswer) {
	log.Printf("[NOTIFICATION SIMULATION] Poll ID %d ended. Results: %+v", pollID, results)
}
