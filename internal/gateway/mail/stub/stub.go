package stub

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"mailpulse/internal/gateway/mail"
	"mailpulse/internal/model"

	"github.com/sirupsen/logrus"
)

// StubProvider is a development stand-in for a real mailbox client.
//
// IT DOES NOT CONNECT TO ANYTHING. It reports credentials as valid, returns a
// fixed folder list, and synthesises a handful of messages so the rest of the
// pipeline — filters, matches, event runs, deliveries — can be exercised end to
// end before an IMAP or Gmail client exists.
//
// Replace it by writing a Provider that speaks IMAP and registering that
// instead in Bootstrap; nothing above this interface changes.
type StubProvider struct {
	Log *logrus.Logger
}

func NewStubProvider(log *logrus.Logger) *StubProvider {
	return &StubProvider{Log: log}
}

func (p *StubProvider) Describe() mail.Descriptor {
	return mail.Descriptor{
		Kind:         mail.KindIMAP,
		Label:        "IMAP (development stub)",
		AuthModes:    []string{mail.AuthPassword, mail.AuthAppPassword},
		Capabilities: mail.Capabilities{Folders: true},
		ConfigSchema: model.Schema{Fields: []model.SchemaField{
			{Name: "host", Label: "IMAP server", Type: "string"},
			{Name: "port", Label: "Port", Type: "int"},
		}},
	}
}

func (p *StubProvider) Verify(ctx context.Context, account mail.Account) ([]mail.Folder, error) {
	p.Log.Warnf("STUB mail provider: pretending %s verified successfully", account.EmailAddress)
	return p.Folders(ctx, account)
}

func (p *StubProvider) Folders(ctx context.Context, account mail.Account) ([]mail.Folder, error) {
	return []mail.Folder{
		{Name: "INBOX", MessageCount: 128},
		{Name: "INBOX/Alerts", MessageCount: 12},
		{Name: "Archive", MessageCount: 940},
		{Name: "Sent", MessageCount: 64},
	}, nil
}

// stubCursor tracks how many synthetic messages this account has already seen,
// so a second sync does not re-deliver the first batch.
type stubCursor struct {
	Delivered int `json:"delivered"`
}

func (p *StubProvider) Fetch(ctx context.Context, account mail.Account, request mail.FetchRequest) (mail.FetchResult, error) {
	p.Log.Warnf("STUB mail provider: synthesising messages for %s, no mailbox was contacted", account.EmailAddress)

	var cursor stubCursor
	if len(request.Cursor) > 0 {
		_ = json.Unmarshal(request.Cursor, &cursor)
	}

	samples := p.samples(account.EmailAddress)

	if cursor.Delivered >= len(samples) {
		next, _ := json.Marshal(cursor)
		return mail.FetchResult{Cursor: next}, nil
	}

	messages := samples[cursor.Delivered:]
	if request.Limit > 0 && len(messages) > request.Limit {
		messages = messages[:request.Limit]
	}

	cursor.Delivered += len(messages)
	next, _ := json.Marshal(cursor)

	return mail.FetchResult{Messages: messages, Cursor: next}, nil
}

func (p *StubProvider) samples(mailbox string) []mail.Message {
	now := time.Now().UnixMilli()

	return []mail.Message{
		{
			MessageID:   fmt.Sprintf("<signup-%d@stub.mailpulse>", now),
			UID:         "1001",
			Subject:     "A new user signed up",
			FromAddress: "notifications@stripe.com",
			FromName:    "Stripe",
			To:          []string{mailbox},
			BodyText:    "Someone signed up for your product a moment ago. Account: acct_1234.",
			Headers:     map[string]string{"X-Mailer": "stub"},
			SizeBytes:   4210,
			ReceivedAt:  now - 60_000,
		},
		{
			MessageID:       fmt.Sprintf("<invoice-%d@stub.mailpulse>", now),
			UID:             "1002",
			Subject:         "Invoice INV-2201 is available",
			FromAddress:     "billing@vendor.example",
			FromName:        "Vendor Billing",
			To:              []string{mailbox},
			BodyText:        "Your invoice for this month is attached and due in 14 days.",
			HasAttachment:   true,
			AttachmentNames: []string{"INV-2201.pdf"},
			SizeBytes:       88400,
			ReceivedAt:      now - 45_000,
		},
		{
			MessageID:   fmt.Sprintf("<newsletter-%d@stub.mailpulse>", now),
			UID:         "1003",
			Subject:     "Weekly digest: what shipped",
			FromAddress: "news@letter.example",
			FromName:    "The Weekly",
			To:          []string{mailbox},
			BodyText:    "Here is everything that shipped this week across the platform.",
			SizeBytes:   15300,
			ReceivedAt:  now - 30_000,
		},
	}
}
