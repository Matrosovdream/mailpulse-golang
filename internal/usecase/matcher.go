package usecase

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/mail"
)

// MatchResult records why a message matched, which is what makes
// "why did this fire?" answerable from the log.
type MatchResult struct {
	Matched      bool
	Descriptions []string
}

// EvaluateFilters applies a watcher's filters to one message. match_mode all
// requires every filter to pass; any requires one. A watcher with no filters
// matches everything, which is deliberate — it is the "notify me on all mail"
// case rather than a misconfiguration.
func EvaluateFilters(watcher *entity.Watcher, filters []entity.WatcherFilter, message *mail.Message) MatchResult {
	if len(filters) == 0 {
		return MatchResult{Matched: true, Descriptions: []string{"no filters: every message matches"}}
	}

	result := MatchResult{Descriptions: []string{}}
	passed := 0

	for i := range filters {
		filter := filters[i]
		if matchFilter(&filter, message) {
			passed++
			result.Descriptions = append(result.Descriptions, describeFilter(&filter))
		}
	}

	if watcher.MatchMode == entity.MatchModeAny {
		result.Matched = passed > 0
	} else {
		result.Matched = passed == len(filters)
	}

	return result
}

func matchFilter(filter *entity.WatcherFilter, message *mail.Message) bool {
	switch filter.Field {
	case entity.FieldHasAttachment:
		want := filter.Value == "" || strings.EqualFold(filter.Value, "true")
		return message.HasAttachment == want

	case entity.FieldSize:
		return compareNumbers(filter.Operator, int64(message.SizeBytes), filter.Value)

	case entity.FieldAttachmentName:
		for _, name := range message.AttachmentNames {
			if compareStrings(filter.Operator, name, filter.Value, filter.CaseSensitive) {
				return true
			}
		}
		return false

	case entity.FieldTo, entity.FieldCc:
		values := message.To
		if filter.Field == entity.FieldCc {
			values = message.Cc
		}
		for _, value := range values {
			if compareStrings(filter.Operator, value, filter.Value, filter.CaseSensitive) {
				return true
			}
		}
		return false

	case entity.FieldHeader:
		if filter.HeaderName == nil {
			return false
		}
		value, ok := lookupHeader(message.Headers, *filter.HeaderName)
		if filter.Operator == entity.OpExists {
			return ok
		}
		return ok && compareStrings(filter.Operator, value, filter.Value, filter.CaseSensitive)

	case entity.FieldSubject:
		return compareStrings(filter.Operator, message.Subject, filter.Value, filter.CaseSensitive)

	case entity.FieldFrom:
		// the sender reads as either the address or the display name
		return compareStrings(filter.Operator, message.FromAddress, filter.Value, filter.CaseSensitive) ||
			compareStrings(filter.Operator, message.FromName, filter.Value, filter.CaseSensitive)

	case entity.FieldBody:
		return compareStrings(filter.Operator, message.BodyText, filter.Value, filter.CaseSensitive)
	}

	return false
}

func compareStrings(operator, subject, value string, caseSensitive bool) bool {
	if !caseSensitive {
		subject = strings.ToLower(subject)
		value = strings.ToLower(value)
	}

	switch operator {
	case entity.OpContains:
		return strings.Contains(subject, value)
	case entity.OpNotContains:
		return !strings.Contains(subject, value)
	case entity.OpEquals:
		return subject == value
	case entity.OpStartsWith:
		return strings.HasPrefix(subject, value)
	case entity.OpEndsWith:
		return strings.HasSuffix(subject, value)
	case entity.OpExists:
		return subject != ""
	case entity.OpRegex:
		// an invalid pattern never matches rather than panicking the worker;
		// the pattern is compiled at write time so this is defence in depth
		expression, err := regexp.Compile(value)
		if err != nil {
			return false
		}
		return expression.MatchString(subject)
	}

	return false
}

func compareNumbers(operator string, subject int64, value string) bool {
	want, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil {
		return false
	}

	switch operator {
	case entity.OpGt:
		return subject > want
	case entity.OpLt:
		return subject < want
	case entity.OpEquals:
		return subject == want
	}

	return false
}

func lookupHeader(headers map[string]string, name string) (string, bool) {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return value, true
		}
	}
	return "", false
}

func describeFilter(filter *entity.WatcherFilter) string {
	field := filter.Field
	if filter.Field == entity.FieldHeader && filter.HeaderName != nil {
		field = "header:" + *filter.HeaderName
	}
	return fmt.Sprintf("%s %s %q", field, strings.ReplaceAll(filter.Operator, "_", " "), filter.Value)
}
