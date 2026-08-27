// Package loadstub is a mail provider whose cost is dialled in rather than
// measured.
//
// It exists because the pipeline cannot be load tested against a real mailbox.
// GreenMail is one container, it is not the thing under test, and its latency
// would dominate every number the harness produced. What matters about a
// mailbox, from the pipeline's point of view, is how long it takes to answer
// and how much it hands back — so those are the knobs, and everything above
// this interface runs unchanged.
//
// It registers under KindIMAP, replacing the real client in the registry it is
// given. That is deliberate: the fixtures then reference the ordinary "imap"
// provider slug and no seed row has to exist for load runs.
package loadstub

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"mailpulse/internal/gateway/mail"
	"mailpulse/internal/model"

	"github.com/sirupsen/logrus"
)

// Config is the mailbox's behaviour.
//
// Latency is a distribution rather than a constant, and that is the single
// most important decision in this package. Work inside a poll cycle is
// processed sequentially, so the tail sets capacity: a constant latency would
// hide the exact failure mode the harness exists to find.
type Config struct {
	// P50 and P99 shape a lognormal dial+fetch time. P99 below P50 is
	// corrected rather than rejected, so a careless flag pair still runs.
	P50 time.Duration
	P99 time.Duration

	// Timeout is what a hung mailbox costs. It should mirror
	// MAIL_IMAP_TIMEOUT, because that is the real ceiling on one bad account.
	Timeout     time.Duration
	TimeoutRate float64

	ErrorRate float64

	MessagesPerSync int
	BodyBytes       int
	// MatchRate is the fraction of synthesised messages that carry a subject
	// the generated filters match, so matches and event runs actually happen.
	MatchRate float64
}

// Provider implements mail.Provider with no network anywhere.
type Provider struct {
	Log    *logrus.Logger
	config Config

	mu     sync.Mutex
	random *rand.Rand

	// counters are read by the suite after a run to check the injected rates
	// landed where they were aimed
	syncs    int64
	timeouts int64
	failures int64
}

func New(log *logrus.Logger, config Config, seed int64) *Provider {
	if config.MessagesPerSync <= 0 {
		config.MessagesPerSync = 5
	}
	if config.BodyBytes <= 0 {
		config.BodyBytes = 2048
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if config.P99 < config.P50 {
		config.P99 = config.P50
	}

	return &Provider{
		Log:    log,
		config: config,
		random: rand.New(rand.NewSource(seed)),
	}
}

func (p *Provider) Describe() mail.Descriptor {
	return mail.Descriptor{
		Kind:         mail.KindIMAP,
		Label:        "IMAP (load stub)",
		AuthModes:    []string{mail.AuthPassword, mail.AuthAppPassword, mail.AuthXOAuth2},
		Capabilities: mail.Capabilities{Folders: true},
		ConfigSchema: model.Schema{Fields: []model.SchemaField{
			{Name: "host", Label: "IMAP server", Type: "string"},
			{Name: "port", Label: "Port", Type: "int"},
		}},
	}
}

// Counts reports what the run actually did, so a suite can state the injected
// rates as observed numbers rather than as the flags it asked for.
func (p *Provider) Counts() (syncs, timeouts, failures int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.syncs, p.timeouts, p.failures
}

func (p *Provider) Verify(ctx context.Context, account mail.Account) ([]mail.Folder, error) {
	if err := p.spend(ctx); err != nil {
		return nil, err
	}
	return p.Folders(ctx, account)
}

func (p *Provider) Folders(ctx context.Context, account mail.Account) ([]mail.Folder, error) {
	return []mail.Folder{
		{Name: "INBOX", MessageCount: 1280},
		{Name: "Archive", MessageCount: 9400},
	}, nil
}

type cursor struct {
	Delivered int `json:"delivered"`
}

func (p *Provider) Fetch(ctx context.Context, account mail.Account, request mail.FetchRequest) (mail.FetchResult, error) {
	p.mu.Lock()
	p.syncs++
	p.mu.Unlock()

	if err := p.spend(ctx); err != nil {
		return mail.FetchResult{}, err
	}

	var state cursor
	if len(request.Cursor) > 0 {
		_ = json.Unmarshal(request.Cursor, &state)
	}

	count := p.config.MessagesPerSync
	if request.Limit > 0 && count > request.Limit {
		count = request.Limit
	}

	now := time.Now().UnixMilli()
	messages := make([]mail.Message, 0, count)

	for i := 0; i < count; i++ {
		sequence := state.Delivered + i
		messages = append(messages, p.message(account, sequence, now))
	}

	state.Delivered += count
	encoded, err := json.Marshal(state)
	if err != nil {
		return mail.FetchResult{}, err
	}

	return mail.FetchResult{Messages: messages, Cursor: encoded}, nil
}

func (p *Provider) message(account mail.Account, sequence int, now int64) mail.Message {
	subject := fmt.Sprintf("load message %d", sequence)
	if p.roll() < p.config.MatchRate {
		// "invoice" and "alert" are what the generated filters look for
		subject = fmt.Sprintf("invoice alert %d", sequence)
	}

	return mail.Message{
		MessageID:   fmt.Sprintf("<load-%s-%d@%s>", account.ID, sequence, "loadstub.invalid"),
		UID:         fmt.Sprintf("%d", sequence+1),
		Subject:     subject,
		FromAddress: "sender@loadstub.invalid",
		FromName:    "Load Sender",
		To:          []string{account.EmailAddress},
		BodyText:    strings.Repeat("load body text. ", p.config.BodyBytes/16+1),
		Headers:     map[string]string{"X-Load-Test": "1"},
		SizeBytes:   p.config.BodyBytes,
		ReceivedAt:  now,
	}
}

// spend is where the configured cost is paid. It honours ctx, so a per-item
// deadline — if the pipeline ever grows one — actually cuts a hung mailbox
// short instead of being ignored by the fake.
func (p *Provider) spend(ctx context.Context) error {
	roll := p.roll()

	switch {
	case roll < p.config.TimeoutRate:
		p.mu.Lock()
		p.timeouts++
		p.mu.Unlock()

		if err := sleep(ctx, p.config.Timeout); err != nil {
			return err
		}
		return fmt.Errorf("loadstub: mailbox timed out after %s", p.config.Timeout)

	case roll < p.config.TimeoutRate+p.config.ErrorRate:
		p.mu.Lock()
		p.failures++
		p.mu.Unlock()

		if err := sleep(ctx, p.latency()); err != nil {
			return err
		}
		return fmt.Errorf("loadstub: injected transient failure")
	}

	return sleep(ctx, p.latency())
}

// latency draws from a lognormal fitted to P50 and P99.
//
// Lognormal rather than uniform because mailbox response times are not
// symmetric: most are quick and a few are dramatically slow, and it is those
// few that decide how many accounts one worker can hold.
func (p *Provider) latency() time.Duration {
	if p.config.P50 <= 0 {
		return 0
	}
	if p.config.P99 <= p.config.P50 {
		return p.config.P50
	}

	// median = exp(mu), and the 99th percentile of a lognormal sits at
	// exp(mu + 2.326*sigma), which pins sigma from the two flags
	mu := math.Log(float64(p.config.P50))
	sigma := (math.Log(float64(p.config.P99)) - mu) / 2.326

	p.mu.Lock()
	normal := p.random.NormFloat64()
	p.mu.Unlock()

	drawn := math.Exp(mu + sigma*normal)
	if drawn > float64(p.config.Timeout) {
		drawn = float64(p.config.Timeout)
	}

	return time.Duration(drawn)
}

func (p *Provider) roll() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.random.Float64()
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
