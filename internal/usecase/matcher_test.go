package usecase

import (
	"testing"

	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/mail"

	"github.com/stretchr/testify/assert"
)

// sample is the message every case below is tested against, so a change to one
// field is visible in every expectation at once.
func sample() *mail.Message {
	return &mail.Message{
		MessageID:       "<abc@corp.com>",
		Subject:         "A new user signed up",
		FromAddress:     "notifications@stripe.com",
		FromName:        "Stripe",
		To:              []string{"inbox@corp.com", "ops@corp.com"},
		Cc:              []string{"audit@corp.com"},
		BodyText:        "Someone signed up. Account acct_1234.",
		Headers:         map[string]string{"List-Id": "alerts.example.com", "X-Mailer": "stub"},
		HasAttachment:   true,
		AttachmentNames: []string{"invoice.pdf"},
		SizeBytes:       4210,
	}
}

func filter(field, operator, value string) entity.WatcherFilter {
	return entity.WatcherFilter{Field: field, Operator: operator, Value: value}
}

func TestEvaluateFilters_Fields(t *testing.T) {
	cases := []struct {
		name   string
		filter entity.WatcherFilter
		want   bool
	}{
		{"subject contains", filter(entity.FieldSubject, entity.OpContains, "signed up"), true},
		{"subject contains miss", filter(entity.FieldSubject, entity.OpContains, "invoice"), false},
		{"subject not_contains", filter(entity.FieldSubject, entity.OpNotContains, "invoice"), true},
		{"subject equals", filter(entity.FieldSubject, entity.OpEquals, "A new user signed up"), true},
		{"subject starts_with", filter(entity.FieldSubject, entity.OpStartsWith, "A new"), true},
		{"subject ends_with", filter(entity.FieldSubject, entity.OpEndsWith, "signed up"), true},
		{"subject regex", filter(entity.FieldSubject, entity.OpRegex, `^A new .* up$`), true},
		{"subject regex miss", filter(entity.FieldSubject, entity.OpRegex, `^invoice`), false},

		// an unparseable pattern must not match, and must not panic the worker
		{"invalid regex never matches", filter(entity.FieldSubject, entity.OpRegex, `([unclosed`), false},

		{"from by address", filter(entity.FieldFrom, entity.OpEndsWith, "@stripe.com"), true},
		{"from by display name", filter(entity.FieldFrom, entity.OpContains, "Stripe"), true},
		{"from miss", filter(entity.FieldFrom, entity.OpContains, "paypal"), false},

		{"to matches any recipient", filter(entity.FieldTo, entity.OpContains, "ops@"), true},
		{"to miss", filter(entity.FieldTo, entity.OpContains, "nobody@"), false},
		{"cc", filter(entity.FieldCc, entity.OpContains, "audit@"), true},

		{"body contains", filter(entity.FieldBody, entity.OpContains, "acct_1234"), true},
		{"body miss", filter(entity.FieldBody, entity.OpContains, "refund"), false},

		{"attachment name", filter(entity.FieldAttachmentName, entity.OpEndsWith, ".pdf"), true},
		{"attachment name miss", filter(entity.FieldAttachmentName, entity.OpEndsWith, ".zip"), false},
		{"has attachment", filter(entity.FieldHasAttachment, entity.OpExists, "true"), true},

		{"size greater", filter(entity.FieldSize, entity.OpGt, "4000"), true},
		{"size greater miss", filter(entity.FieldSize, entity.OpGt, "9000"), false},
		{"size less", filter(entity.FieldSize, entity.OpLt, "9000"), true},
		{"size equals", filter(entity.FieldSize, entity.OpEquals, "4210"), true},
		{"size with junk value", filter(entity.FieldSize, entity.OpGt, "not-a-number"), false},

		{"unknown field never matches", filter("nonsense", entity.OpContains, "x"), false},
	}

	watcher := &entity.Watcher{MatchMode: entity.MatchModeAll}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := EvaluateFilters(watcher, []entity.WatcherFilter{testCase.filter}, sample())
			assert.Equal(t, testCase.want, result.Matched)
		})
	}
}

func TestEvaluateFilters_Header(t *testing.T) {
	name := "list-id"
	headerFilter := entity.WatcherFilter{
		Field: entity.FieldHeader, HeaderName: &name,
		Operator: entity.OpContains, Value: "alerts",
	}

	watcher := &entity.Watcher{MatchMode: entity.MatchModeAll}

	// header lookup is case-insensitive on the header name
	assert.True(t, EvaluateFilters(watcher, []entity.WatcherFilter{headerFilter}, sample()).Matched)

	missing := "X-Nope"
	headerFilter.HeaderName = &missing
	assert.False(t, EvaluateFilters(watcher, []entity.WatcherFilter{headerFilter}, sample()).Matched)

	// a header filter with no header name is a misconfiguration, not a match
	headerFilter.HeaderName = nil
	assert.False(t, EvaluateFilters(watcher, []entity.WatcherFilter{headerFilter}, sample()).Matched)
}

func TestEvaluateFilters_CaseSensitivity(t *testing.T) {
	watcher := &entity.Watcher{MatchMode: entity.MatchModeAll}

	insensitive := filter(entity.FieldSubject, entity.OpContains, "SIGNED UP")
	assert.True(t, EvaluateFilters(watcher, []entity.WatcherFilter{insensitive}, sample()).Matched,
		"filters are case-insensitive by default")

	sensitive := insensitive
	sensitive.CaseSensitive = true
	assert.False(t, EvaluateFilters(watcher, []entity.WatcherFilter{sensitive}, sample()).Matched)
}

func TestEvaluateFilters_MatchMode(t *testing.T) {
	hit := filter(entity.FieldSubject, entity.OpContains, "signed up")
	miss := filter(entity.FieldSubject, entity.OpContains, "invoice")

	all := &entity.Watcher{MatchMode: entity.MatchModeAll}
	any := &entity.Watcher{MatchMode: entity.MatchModeAny}

	assert.True(t, EvaluateFilters(all, []entity.WatcherFilter{hit, hit}, sample()).Matched)
	assert.False(t, EvaluateFilters(all, []entity.WatcherFilter{hit, miss}, sample()).Matched,
		"all requires every filter to pass")

	assert.True(t, EvaluateFilters(any, []entity.WatcherFilter{hit, miss}, sample()).Matched,
		"any requires one filter to pass")
	assert.False(t, EvaluateFilters(any, []entity.WatcherFilter{miss, miss}, sample()).Matched)

	// an empty mode behaves like all, since that is the column default
	assert.False(t, EvaluateFilters(&entity.Watcher{}, []entity.WatcherFilter{hit, miss}, sample()).Matched)
}

func TestEvaluateFilters_NoFiltersMatchesEverything(t *testing.T) {
	result := EvaluateFilters(&entity.Watcher{MatchMode: entity.MatchModeAll}, nil, sample())

	assert.True(t, result.Matched, "a watcher with no filters is 'notify me on all mail'")
	assert.NotEmpty(t, result.Descriptions)
}

// The descriptions are what the UI shows as "why did this fire?", so they are
// part of the contract rather than debug output.
func TestEvaluateFilters_RecordsWhyItMatched(t *testing.T) {
	filters := []entity.WatcherFilter{
		filter(entity.FieldSubject, entity.OpContains, "signed up"),
		filter(entity.FieldFrom, entity.OpEndsWith, "@stripe.com"),
	}

	result := EvaluateFilters(&entity.Watcher{MatchMode: entity.MatchModeAll}, filters, sample())

	assert.True(t, result.Matched)
	assert.Len(t, result.Descriptions, 2)
	assert.Contains(t, result.Descriptions[0], "subject contains")
	assert.Contains(t, result.Descriptions[0], "signed up")
}
