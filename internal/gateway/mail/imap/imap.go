package imap

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	goimap "github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	gomessage "github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // registers non-UTF-8 charsets
	gomail "github.com/emersion/go-message/mail"
	"github.com/sirupsen/logrus"
	"mailpulse/internal/gateway/mail"
	"mailpulse/internal/model"
)

// IMAPProvider reads a real mailbox over IMAP.
//
// It is deliberately read-only: the mailbox is SELECTed in readonly mode and
// bodies are fetched with BODY.PEEK, so watching a mailbox never marks anything
// as read or otherwise changes what the owner sees.
type IMAPProvider struct {
	Log     *logrus.Logger
	Timeout time.Duration
}

func NewIMAPProvider(log *logrus.Logger, timeout time.Duration) *IMAPProvider {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &IMAPProvider{Log: log, Timeout: timeout}
}

// Settings is the provider-specific half of a mail_accounts row.
type Settings struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	UseTLS *bool  `json:"use_tls"`
}

func (p *IMAPProvider) Describe() mail.Descriptor {
	return mail.Descriptor{
		Kind:      mail.KindIMAP,
		Label:     "IMAP",
		AuthModes: []string{mail.AuthPassword, mail.AuthAppPassword},
		Capabilities: mail.Capabilities{
			Folders:      true,
			Idle:         true,
			ServerSearch: true,
		},
		ConfigSchema: model.Schema{Fields: []model.SchemaField{
			{Name: "host", Label: "IMAP server", Type: "string", Required: true, Placeholder: "imap.example.com"},
			{Name: "port", Label: "Port", Type: "int", Required: true, Placeholder: "993"},
			{Name: "use_tls", Label: "Use TLS", Type: "bool",
				Help: "On for port 993. Off for 143, which is upgraded with STARTTLS when the server offers it."},
			{Name: "username", Label: "Username", Type: "string", Secret: true,
				Help: "Only needed when the login differs from the email address."},
			{Name: "password", Label: "Password", Type: "secret", Required: true, Secret: true,
				Help: "Most providers require an app-specific password rather than the account password."},
		}},
	}
}

// imapCursor is what gets stored in mail_accounts.sync_state.
//
// UIDs are only meaningful within one UIDVALIDITY. When a server renumbers a
// mailbox it bumps UIDVALIDITY, and any stored UID becomes meaningless — so the
// cursor is reset rather than used to skip mail that was never seen.
type imapCursor struct {
	UIDValidity uint32 `json:"uid_validity"`
	LastUID     uint32 `json:"last_uid"`
}

func (p *IMAPProvider) connect(account mail.Account) (*client.Client, error) {
	var settings Settings
	if err := account.DecodeSettings(&settings); err != nil {
		return nil, fmt.Errorf("imap: settings are not readable: %w", err)
	}

	if settings.Host == "" || settings.Port == 0 {
		return nil, fmt.Errorf("imap: this account has no host or port configured")
	}

	useTLS := settings.Port == 993
	if settings.UseTLS != nil {
		useTLS = *settings.UseTLS
	}

	address := fmt.Sprintf("%s:%d", settings.Host, settings.Port)
	dialer := &net.Dialer{Timeout: p.Timeout}

	var connection *client.Client
	var err error

	if useTLS {
		// implicit TLS, the usual 993
		connection, err = client.DialWithDialerTLS(dialer, address, &tls.Config{ServerName: settings.Host})
		if err != nil {
			return nil, fmt.Errorf("imap: cannot reach %s: %w", address, err)
		}
	} else {
		connection, err = client.DialWithDialer(dialer, address)
		if err != nil {
			return nil, fmt.Errorf("imap: cannot reach %s: %w", address, err)
		}

		// A cleartext port that offers STARTTLS must be upgraded before LOGIN,
		// or the account password crosses the wire in the clear. Yandex, Gmail
		// and most hosts advertise it on 143.
		if supported, _ := connection.SupportStartTLS(); supported {
			if err := connection.StartTLS(&tls.Config{ServerName: settings.Host}); err != nil {
				_ = connection.Logout()
				return nil, fmt.Errorf("imap: STARTTLS upgrade failed for %s: %w", address, err)
			}
		} else {
			p.Log.Warnf("imap: %s offers no TLS on %s, sending credentials in cleartext",
				account.EmailAddress, address)
		}
	}

	connection.Timeout = p.Timeout

	username := account.Credentials.Username
	if username == "" {
		username = account.EmailAddress
	}

	// XOAUTH2 is IMAP transport with an OAuth token; it belongs here rather
	// than in a separate provider, but the SASL mechanism is not wired yet
	if account.AuthMode == mail.AuthOAuth2 || account.AuthMode == mail.AuthXOAuth2 {
		_ = connection.Logout()
		return nil, fmt.Errorf("imap: %s is not implemented yet, use an app password", account.AuthMode)
	}

	if err := connection.Login(username, account.Credentials.Password); err != nil {
		_ = connection.Logout()
		// the server's own wording is the most useful thing to show the user
		return nil, fmt.Errorf("imap: login failed for %s: %w", username, err)
	}

	return connection, nil
}

func (p *IMAPProvider) Verify(ctx context.Context, account mail.Account) ([]mail.Folder, error) {
	connection, err := p.connect(account)
	if err != nil {
		return nil, err
	}
	defer connection.Logout()

	return p.listFolders(connection)
}

func (p *IMAPProvider) Folders(ctx context.Context, account mail.Account) ([]mail.Folder, error) {
	return p.Verify(ctx, account)
}

// listFolders returns the mailboxes, with counts for a bounded number of them:
// STATUS is a round trip each, and an account with hundreds of folders should
// not turn one page load into hundreds of commands.
func (p *IMAPProvider) listFolders(connection *client.Client) ([]mail.Folder, error) {
	mailboxes := make(chan *goimap.MailboxInfo, 32)
	done := make(chan error, 1)

	go func() { done <- connection.List("", "*", mailboxes) }()

	var folders []mail.Folder
	for mailbox := range mailboxes {
		if hasAttribute(mailbox.Attributes, goimap.NoSelectAttr) {
			continue
		}
		folders = append(folders, mail.Folder{Name: mailbox.Name})
	}

	if err := <-done; err != nil {
		return nil, fmt.Errorf("imap: cannot list folders: %w", err)
	}

	const withCounts = 50
	for i := range folders {
		if i >= withCounts {
			break
		}
		status, err := connection.Status(folders[i].Name, []goimap.StatusItem{goimap.StatusMessages})
		if err == nil && status != nil {
			folders[i].MessageCount = int(status.Messages)
		}
	}

	return folders, nil
}

func (p *IMAPProvider) Fetch(ctx context.Context, account mail.Account, request mail.FetchRequest) (mail.FetchResult, error) {
	connection, err := p.connect(account)
	if err != nil {
		return mail.FetchResult{}, err
	}
	defer connection.Logout()

	folder := request.Folder
	if folder == "" {
		folder = "INBOX"
	}

	// readonly: watching must not mark anything as seen
	mailbox, err := connection.Select(folder, true)
	if err != nil {
		return mail.FetchResult{}, fmt.Errorf("imap: cannot open folder %q: %w", folder, err)
	}

	var cursor imapCursor
	if len(request.Cursor) > 0 {
		_ = json.Unmarshal(request.Cursor, &cursor)
	}

	if cursor.UIDValidity != 0 && cursor.UIDValidity != mailbox.UidValidity {
		p.Log.Warnf("imap: %s renumbered %s (uidvalidity %d -> %d), restarting the cursor",
			account.EmailAddress, folder, cursor.UIDValidity, mailbox.UidValidity)
		cursor = imapCursor{}
	}
	cursor.UIDValidity = mailbox.UidValidity

	if mailbox.Messages == 0 {
		return mail.FetchResult{Cursor: encodeCursor(cursor)}, nil
	}

	limit := request.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}

	messages, err := p.fetchMessages(connection, mailbox, cursor, limit)
	if err != nil {
		return mail.FetchResult{}, err
	}

	for i := range messages {
		if uid := parseUID(messages[i].UID); uid > cursor.LastUID {
			cursor.LastUID = uid
		}
	}

	return mail.FetchResult{Messages: messages, Cursor: encodeCursor(cursor)}, nil
}

func (p *IMAPProvider) fetchMessages(connection *client.Client, mailbox *goimap.MailboxStatus,
	cursor imapCursor, limit int) ([]mail.Message, error) {
	section := &goimap.BodySectionName{Peek: true}
	items := []goimap.FetchItem{
		goimap.FetchEnvelope,
		goimap.FetchUid,
		goimap.FetchRFC822Size,
		goimap.FetchInternalDate,
		section.FetchItem(),
	}

	incoming := make(chan *goimap.Message, 16)
	done := make(chan error, 1)

	if cursor.LastUID == 0 {
		// first run: take the newest `limit` by sequence number rather than
		// dragging an entire mailbox through the matcher
		from := uint32(1)
		if mailbox.Messages > uint32(limit) {
			from = mailbox.Messages - uint32(limit) + 1
		}
		set := new(goimap.SeqSet)
		set.AddRange(from, mailbox.Messages)
		go func() { done <- connection.Fetch(set, items, incoming) }()
	} else {
		// everything after the last UID we recorded
		set := new(goimap.SeqSet)
		set.AddRange(cursor.LastUID+1, 0) // 0 means "*"
		go func() { done <- connection.UidFetch(set, items, incoming) }()
	}

	var messages []mail.Message
	for raw := range incoming {
		// "N:*" is inclusive of the highest UID even when N is past it, so a
		// server answers 4:* with message 3 when 3 is the newest. Without this
		// guard every poll re-fetches the newest message forever; the dedupe
		// index hides it, but the work is real.
		if cursor.LastUID > 0 && raw.Uid <= cursor.LastUID {
			continue
		}

		message, err := p.convert(raw, section)
		if err != nil {
			p.Log.WithError(err).Warnf("imap: skipping a message that would not parse (uid %d)", raw.Uid)
			continue
		}
		messages = append(messages, message)
	}

	if err := <-done; err != nil {
		return nil, fmt.Errorf("imap: fetch failed: %w", err)
	}

	return messages, nil
}

// convert flattens an IMAP message into the parts the filters can test.
func (p *IMAPProvider) convert(raw *goimap.Message, section *goimap.BodySectionName) (mail.Message, error) {
	if raw.Envelope == nil {
		return mail.Message{}, fmt.Errorf("message has no envelope")
	}

	message := mail.Message{
		UID:        fmt.Sprint(raw.Uid),
		Subject:    raw.Envelope.Subject,
		MessageID:  strings.TrimSpace(raw.Envelope.MessageId),
		SizeBytes:  int(raw.Size),
		ReceivedAt: raw.InternalDate.UnixMilli(),
		Headers:    map[string]string{},
	}

	if !raw.Envelope.Date.IsZero() {
		message.ReceivedAt = raw.Envelope.Date.UnixMilli()
	}

	// a server that omits Message-ID would otherwise defeat the dedupe index,
	// so fall back to something stable for this mailbox
	if message.MessageID == "" {
		message.MessageID = fmt.Sprintf("<uid-%d@mailpulse.local>", raw.Uid)
	}

	if len(raw.Envelope.From) > 0 {
		message.FromAddress = raw.Envelope.From[0].Address()
		message.FromName = raw.Envelope.From[0].PersonalName
	}
	for _, address := range raw.Envelope.To {
		message.To = append(message.To, address.Address())
	}
	for _, address := range raw.Envelope.Cc {
		message.Cc = append(message.Cc, address.Address())
	}

	body := raw.GetBody(section)
	if body == nil {
		return message, nil
	}

	if err := p.readBody(&message, body); err != nil {
		// headers and envelope are still useful even when the body defeats us
		p.Log.WithError(err).Debugf("imap: could not parse the body of uid %d", raw.Uid)
	}

	return message, nil
}

func (p *IMAPProvider) readBody(message *mail.Message, body io.Reader) error {
	reader, err := gomail.CreateReader(body)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, field := range headerFields(reader.Header.Header) {
		message.Headers[field.key] = field.value
	}

	var plain, html string

	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		switch header := part.Header.(type) {
		case *gomail.InlineHeader:
			contentType, _, _ := header.ContentType()
			content, readErr := io.ReadAll(io.LimitReader(part.Body, 1<<20))
			if readErr != nil {
				continue
			}
			if strings.HasPrefix(contentType, "text/plain") && plain == "" {
				plain = string(content)
			} else if strings.HasPrefix(contentType, "text/html") && html == "" {
				html = string(content)
			}

		case *gomail.AttachmentHeader:
			message.HasAttachment = true
			if filename, err := header.Filename(); err == nil && filename != "" {
				message.AttachmentNames = append(message.AttachmentNames, filename)
			}
		}
	}

	// prefer the plain part; fall back to html so a body filter still has
	// something to match on for html-only senders
	message.BodyText = plain
	if message.BodyText == "" {
		message.BodyText = html
	}

	return nil
}

type headerField struct{ key, value string }

func headerFields(header gomessage.Header) []headerField {
	var fields []headerField

	iterator := header.Fields()
	for iterator.Next() {
		value, err := iterator.Text()
		if err != nil {
			value = ""
		}
		fields = append(fields, headerField{key: iterator.Key(), value: value})

		// a header filter needs the usual suspects, not an unbounded map
		if len(fields) >= 60 {
			break
		}
	}

	return fields
}

func encodeCursor(cursor imapCursor) []byte {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return nil
	}
	return encoded
}

func parseUID(value string) uint32 {
	var uid uint32
	if _, err := fmt.Sscanf(value, "%d", &uid); err != nil {
		return 0
	}
	return uid
}

func hasAttribute(attributes []string, want string) bool {
	for _, attribute := range attributes {
		if strings.EqualFold(attribute, want) {
			return true
		}
	}
	return false
}
