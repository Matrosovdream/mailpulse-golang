package imap

import (
	"bytes"
	"strings"

	"mailpulse/internal/gateway/mail"

	gomail "github.com/emersion/go-message/mail"
	"github.com/sirupsen/logrus"
)

// ParseMessage decodes one raw RFC822 message into the provider-agnostic
// shape, with no IMAP connection anywhere.
//
// This is the seam the MIME work is reachable through from outside the
// package. Over a connection the envelope arrives pre-parsed from the server
// and only the body is decoded here; given raw bytes there is no envelope, so
// the subject and addresses are read back off the parsed header instead. The
// decoding itself — transfer encodings, charsets, multipart walking,
// attachments — is the same code either way, and it is the part whose cost is
// worth measuring.
func ParseMessage(log *logrus.Logger, raw []byte) (mail.Message, error) {
	if log == nil {
		log = logrus.New()
		log.SetOutput(discard{})
	}

	message := mail.Message{
		Headers:   map[string]string{},
		SizeBytes: len(raw),
	}

	provider := &IMAPProvider{Log: log}
	if err := provider.readBody(&message, bytes.NewReader(raw)); err != nil {
		return message, err
	}

	fillEnvelope(&message, raw)

	return message, nil
}

// fillEnvelope recovers the fields the IMAP path would have taken from the
// server's ENVELOPE, so a message parsed from bytes is shaped like one that
// arrived over a connection.
func fillEnvelope(message *mail.Message, raw []byte) {
	message.Subject = message.Headers["Subject"]

	reader, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return
	}
	defer reader.Close()

	if addresses, err := reader.Header.AddressList("From"); err == nil && len(addresses) > 0 {
		message.FromAddress = addresses[0].Address
		message.FromName = addresses[0].Name
	}
	for _, field := range []string{"To", "Cc"} {
		addresses, err := reader.Header.AddressList(field)
		if err != nil {
			continue
		}
		for _, address := range addresses {
			if field == "To" {
				message.To = append(message.To, address.Address)
			} else {
				message.Cc = append(message.Cc, address.Address)
			}
		}
	}

	if id := strings.TrimSpace(message.Headers["Message-Id"]); id != "" {
		message.MessageID = id
	}
}

// discard silences a parser used without a logger, which is the normal case
// for a benchmark.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
