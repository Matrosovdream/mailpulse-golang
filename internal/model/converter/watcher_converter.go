package converter

import (
	"encoding/json"

	"mailpulse/internal/entity"
	"mailpulse/internal/model"
)

func WatcherToResponse(watcher *entity.Watcher) *model.WatcherResponse {
	response := &model.WatcherResponse{
		ID:              watcher.ID,
		Name:            watcher.Name,
		Description:     watcher.Description,
		Status:          watcher.Status,
		MatchMode:       watcher.MatchMode,
		Folder:          watcher.Folder,
		WatchFrom:       watcher.WatchFrom,
		CooldownSeconds: watcher.CooldownSeconds,
		LastMatchedAt:   watcher.LastMatchedAt,
		MatchCount:      watcher.MatchCount,
		ArchivedAt:      watcher.ArchivedAt,
		MailAccountID:   watcher.MailAccountID,
		CreatedAt:       watcher.CreatedAt,
		UpdatedAt:       watcher.UpdatedAt,
	}

	if watcher.MailAccount != nil {
		response.MailAccount = MailAccountToResponse(watcher.MailAccount)
	}

	response.Filters = FiltersToResponses(watcher.Filters)

	if len(watcher.Events) > 0 {
		response.Events = EventsToResponses(watcher.Events)
	}

	return response
}

func WatchersToResponses(watchers []entity.Watcher) []model.WatcherResponse {
	responses := make([]model.WatcherResponse, 0, len(watchers))
	for i := range watchers {
		responses = append(responses, *WatcherToResponse(&watchers[i]))
	}
	return responses
}

func FilterToResponse(filter *entity.WatcherFilter) *model.WatcherFilterResponse {
	return &model.WatcherFilterResponse{
		ID:            filter.ID,
		Field:         filter.Field,
		HeaderName:    filter.HeaderName,
		Operator:      filter.Operator,
		Value:         filter.Value,
		CaseSensitive: filter.CaseSensitive,
		Position:      filter.Position,
	}
}

func FiltersToResponses(filters []entity.WatcherFilter) []model.WatcherFilterResponse {
	responses := make([]model.WatcherFilterResponse, 0, len(filters))
	for i := range filters {
		responses = append(responses, *FilterToResponse(&filters[i]))
	}
	return responses
}

func EventToResponse(event *entity.WatcherEvent) *model.WatcherEventResponse {
	config := json.RawMessage(event.Config)
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}

	return &model.WatcherEventResponse{
		ID:                    event.ID,
		WatcherID:             event.WatcherID,
		Type:                  event.Type,
		Config:                config,
		Position:              event.Position,
		Enabled:               event.Enabled,
		RunMode:               event.RunMode,
		DelaySeconds:          event.DelaySeconds,
		RepeatIntervalSeconds: event.RepeatIntervalSeconds,
		RepeatMax:             event.RepeatMax,
		RepeatUntil:           event.RepeatUntil,
		CronExpression:        event.CronExpression,
		StopOnAck:             event.StopOnAck,
		Notifiers:             NotifiersToResponses(event.Notifiers),
		CreatedAt:             event.CreatedAt,
		UpdatedAt:             event.UpdatedAt,
	}
}

func EventsToResponses(events []entity.WatcherEvent) []model.WatcherEventResponse {
	responses := make([]model.WatcherEventResponse, 0, len(events))
	for i := range events {
		responses = append(responses, *EventToResponse(&events[i]))
	}
	return responses
}
