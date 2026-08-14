package converter

import (
	"encoding/json"

	"mailpulse/internal/entity"
	"mailpulse/internal/model"
)

// NotifierToResponse returns config but never secrets.
func NotifierToResponse(notifier *entity.Notifier) *model.NotifierResponse {
	config := json.RawMessage(notifier.Config)
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}

	return &model.NotifierResponse{
		ID:         notifier.ID,
		Type:       notifier.Type,
		Name:       notifier.Name,
		Config:     config,
		Status:     notifier.Status,
		VerifiedAt: notifier.VerifiedAt,
		LastError:  notifier.LastError,
		IsDefault:  notifier.IsDefault,
		CreatedAt:  notifier.CreatedAt,
		UpdatedAt:  notifier.UpdatedAt,
	}
}

func NotifiersToResponses(notifiers []entity.Notifier) []model.NotifierResponse {
	responses := make([]model.NotifierResponse, 0, len(notifiers))
	for i := range notifiers {
		responses = append(responses, *NotifierToResponse(&notifiers[i]))
	}
	return responses
}
