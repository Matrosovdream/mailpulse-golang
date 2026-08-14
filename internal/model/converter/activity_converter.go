package converter

import (
	"encoding/json"

	"mailpulse/internal/entity"
	"mailpulse/internal/model"
)

func MatchedEmailToResponse(match *entity.MatchedEmail) *model.MatchedEmailResponse {
	response := &model.MatchedEmailResponse{
		ID:            match.ID,
		WatcherID:     match.WatcherID,
		MailAccountID: match.MailAccountID,
		MessageID:     match.MessageID,
		Subject:       match.Subject,
		FromAddress:   match.FromAddress,
		FromName:      match.FromName,
		ToAddresses:   match.ToAddresses,
		Snippet:       match.Snippet,
		HasAttachment: match.HasAttachment,
		SizeBytes:     match.SizeBytes,
		ReceivedAt:    match.ReceivedAt,
		MatchedAt:     match.MatchedAt,
	}

	if len(match.MatchedFilters) > 0 {
		response.MatchedFilters = json.RawMessage(match.MatchedFilters)
	}
	if len(match.Runs) > 0 {
		response.Runs = EventRunsToResponses(match.Runs)
	}

	return response
}

func MatchedEmailsToResponses(matches []entity.MatchedEmail) []model.MatchedEmailResponse {
	responses := make([]model.MatchedEmailResponse, 0, len(matches))
	for i := range matches {
		responses = append(responses, *MatchedEmailToResponse(&matches[i]))
	}
	return responses
}

func EventRunToResponse(run *entity.EventRun) *model.EventRunResponse {
	response := &model.EventRunResponse{
		ID:             run.ID,
		MatchedEmailID: run.MatchedEmailID,
		WatcherEventID: run.WatcherEventID,
		Occurrence:     run.Occurrence,
		Status:         run.Status,
		ScheduledAt:    run.ScheduledAt,
		StartedAt:      run.StartedAt,
		FinishedAt:     run.FinishedAt,
		Attempt:        run.Attempt,
		MaxAttempts:    run.MaxAttempts,
		Error:          run.Error,
		AcknowledgedAt: run.AcknowledgedAt,
		CreatedAt:      run.CreatedAt,
	}

	if len(run.ConfigSnapshot) > 0 {
		response.ConfigSnapshot = json.RawMessage(run.ConfigSnapshot)
	}
	if len(run.Result) > 0 {
		response.Result = json.RawMessage(run.Result)
	}
	if len(run.Deliveries) > 0 {
		response.Deliveries = DeliveriesToResponses(run.Deliveries)
	}

	return response
}

func EventRunsToResponses(runs []entity.EventRun) []model.EventRunResponse {
	responses := make([]model.EventRunResponse, 0, len(runs))
	for i := range runs {
		responses = append(responses, *EventRunToResponse(&runs[i]))
	}
	return responses
}

func DeliveryToResponse(delivery *entity.NotificationDelivery) *model.DeliveryResponse {
	return &model.DeliveryResponse{
		ID:                delivery.ID,
		EventRunID:        delivery.EventRunID,
		NotifierID:        delivery.NotifierID,
		ChannelType:       delivery.ChannelType,
		Status:            delivery.Status,
		RenderedMessage:   delivery.RenderedMessage,
		ProviderMessageID: delivery.ProviderMessageID,
		Error:             delivery.Error,
		SentAt:            delivery.SentAt,
		CreatedAt:         delivery.CreatedAt,
	}
}

func DeliveriesToResponses(deliveries []entity.NotificationDelivery) []model.DeliveryResponse {
	responses := make([]model.DeliveryResponse, 0, len(deliveries))
	for i := range deliveries {
		responses = append(responses, *DeliveryToResponse(&deliveries[i]))
	}
	return responses
}

func AuditLogToResponse(log *entity.AuditLog, actorEmail string) *model.AuditLogResponse {
	response := &model.AuditLogResponse{
		ID:                 log.ID,
		ActorUserID:        log.ActorUserID,
		ActorEmail:         actorEmail,
		ImpersonatedUserID: log.ImpersonatedUserID,
		Action:             log.Action,
		EntityType:         log.EntityType,
		EntityID:           log.EntityID,
		IP:                 log.IP,
		CreatedAt:          log.CreatedAt,
	}

	if len(log.Metadata) > 0 {
		response.Metadata = json.RawMessage(log.Metadata)
	}

	return response
}
