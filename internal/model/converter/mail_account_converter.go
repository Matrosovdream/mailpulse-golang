package converter

import (
	"mailpulse/internal/entity"
	"mailpulse/internal/model"
)

// MailAccountToResponse deliberately drops credentials and sync_state: one is
// secret, the other is provider bookkeeping the SPA has no use for.
func MailAccountToResponse(account *entity.MailAccount) *model.MailAccountResponse {
	return &model.MailAccountResponse{
		ID:                  account.ID,
		Provider:            account.Provider,
		EmailAddress:        account.EmailAddress,
		DisplayName:         account.DisplayName,
		AuthType:            account.AuthType,
		ImapHost:            account.ImapHost,
		ImapPort:            account.ImapPort,
		ImapUseTLS:          account.ImapUseTLS,
		Status:              account.Status,
		LastVerifiedAt:      account.LastVerifiedAt,
		LastError:           account.LastError,
		LastSyncedAt:        account.LastSyncedAt,
		PollIntervalSeconds: account.PollIntervalSeconds,
		NextPollAt:          account.NextPollAt,
		CreatedAt:           account.CreatedAt,
		UpdatedAt:           account.UpdatedAt,
	}
}

func MailAccountsToResponses(accounts []entity.MailAccount) []model.MailAccountResponse {
	responses := make([]model.MailAccountResponse, 0, len(accounts))
	for i := range accounts {
		responses = append(responses, *MailAccountToResponse(&accounts[i]))
	}
	return responses
}

func SyncRunToResponse(run *entity.MailSyncRun) *model.MailSyncRunResponse {
	return &model.MailSyncRunResponse{
		ID:              run.ID,
		Status:          run.Status,
		StartedAt:       run.StartedAt,
		FinishedAt:      run.FinishedAt,
		MessagesFetched: run.MessagesFetched,
		MatchesCreated:  run.MatchesCreated,
		Error:           run.Error,
	}
}

func SyncRunsToResponses(runs []entity.MailSyncRun) []model.MailSyncRunResponse {
	responses := make([]model.MailSyncRunResponse, 0, len(runs))
	for i := range runs {
		responses = append(responses, *SyncRunToResponse(&runs[i]))
	}
	return responses
}
