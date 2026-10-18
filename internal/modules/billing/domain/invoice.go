package domain

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// GenerateInvoiceNumber builds a unique invoice number.
func GenerateInvoiceNumber(now time.Time) string {
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	return "INV-" + strings.ToUpper(hex.EncodeToString(buf)) + "-" + now.Format("20060102150405")
}
