package usecase

import (
	"context"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/internal/model"
	"mailpulse/internal/model/converter"
	"mailpulse/internal/repository"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// AdminUseCase is the superadmin surface. It reuses the same repositories as
// the user-facing usecases with the ownership filter lifted, rather than a
// parallel set of queries that could drift.
type AdminUseCase struct {
	DB        *gorm.DB
	Log       *logrus.Logger
	Validate  *validator.Validate
	Users     *repository.UserRepository
	Roles     *repository.RoleRepository
	Sessions  *repository.UserSessionRepository
	Watchers  *repository.WatcherRepository
	Accounts  *repository.MailAccountRepository
	Notifiers *repository.NotifierRepository
	Matches   *repository.MatchedEmailRepository
	Runs      *repository.EventRunRepository
	Audit     *AuditUseCase
	UserUC    *UserUseCase
}

func NewAdminUseCase(db *gorm.DB, log *logrus.Logger, validate *validator.Validate,
	users *repository.UserRepository, roles *repository.RoleRepository,
	sessions *repository.UserSessionRepository, watchers *repository.WatcherRepository,
	accounts *repository.MailAccountRepository, notifiers *repository.NotifierRepository,
	matches *repository.MatchedEmailRepository, runs *repository.EventRunRepository,
	audit *AuditUseCase, userUC *UserUseCase) *AdminUseCase {
	return &AdminUseCase{
		DB: db, Log: log, Validate: validate,
		Users: users, Roles: roles, Sessions: sessions, Watchers: watchers,
		Accounts: accounts, Notifiers: notifiers, Matches: matches, Runs: runs,
		Audit: audit, UserUC: userUC,
	}
}

func (c *AdminUseCase) ListUsers(ctx context.Context, request *model.ListAdminUserRequest) ([]model.AdminUserResponse, *model.PageMetadata, error) {
	request.Normalize()
	if err := c.Validate.Struct(request); err != nil {
		return nil, nil, fiber.ErrBadRequest
	}

	db := c.DB.WithContext(ctx)
	query := c.Users.Search(db, request.Query, request.Role, request.Status)

	var users []entity.User
	total, err := c.Users.Paginate(query, &users, request.Page, request.Size)
	if err != nil {
		c.Log.Warnf("Failed list users : %+v", err)
		return nil, nil, fiber.ErrInternalServerError
	}

	if err := c.Users.LoadRolesForAll(db, users); err != nil {
		c.Log.Warnf("Failed load roles : %+v", err)
	}

	responses := make([]model.AdminUserResponse, 0, len(users))
	for i := range users {
		responses = append(responses, model.AdminUserResponse{
			UserResponse: *converter.UserToResponse(&users[i]),
			Counts:       c.countsFor(db, users[i].ID),
		})
	}

	metadata := model.NewPageMetadata(request.Page, request.Size, total)
	return responses, &metadata, nil
}

func (c *AdminUseCase) countsFor(db *gorm.DB, userID string) model.AdminUserCounts {
	counts := model.AdminUserCounts{}
	counts.Watchers, _ = c.Watchers.CountForUser(db, userID)
	counts.MailAccounts, _ = c.Accounts.CountForUser(db, userID)
	counts.Notifiers, _ = c.Notifiers.CountForUser(db, userID)
	counts.Matches, _ = c.Matches.CountForUser(db, userID)
	return counts
}

func (c *AdminUseCase) GetUser(ctx context.Context, request *model.GetAdminUserRequest) (*model.AdminUserResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	db := c.DB.WithContext(ctx)

	user := new(entity.User)
	if err := c.Users.FindById(db, user, request.ID); err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "user not found")
	}

	if err := c.Users.LoadRoles(db, user); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return &model.AdminUserResponse{
		UserResponse: *converter.UserToResponse(user),
		Counts:       c.countsFor(db, user.ID),
	}, nil
}

// UpdateUser is where a superadmin is granted or revoked, so it evicts the
// target's cached sessions: their roles ride along in the cached Auth.
func (c *AdminUseCase) UpdateUser(ctx context.Context, request *model.UpdateAdminUserRequest) (*model.AdminUserResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	user := new(entity.User)
	if err := c.Users.FindById(tx, user, request.ID); err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "user not found")
	}

	if request.Name != "" {
		user.Name = request.Name
		if err := c.Users.Update(tx, user); err != nil {
			return nil, fiber.ErrInternalServerError
		}
	}

	rolesChanged := false
	if request.Roles != nil {
		roles, err := c.Roles.FindBySlugs(tx, request.Roles)
		if err != nil {
			return nil, fiber.ErrInternalServerError
		}
		if len(roles) != len(request.Roles) {
			return nil, fiber.NewError(fiber.StatusBadRequest, "one or more roles do not exist")
		}

		ids := make([]string, 0, len(roles))
		for i := range roles {
			ids = append(ids, roles[i].ID)
		}

		if err := c.Users.ReplaceRoles(tx, user.ID, ids); err != nil {
			return nil, fiber.ErrInternalServerError
		}
		rolesChanged = true
	}

	c.Audit.Record(ctx, tx, AuditEntry{ActorID: &request.ActorID, Action: "admin.user.updated",
		EntityType: "users", EntityID: &user.ID, Metadata: map[string]any{"roles": request.Roles}})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	if rolesChanged {
		// a stale cached Auth would keep the old roles until the TTL lapsed
		c.UserUC.revokeAllSessions(ctx, user.ID)
	}

	return c.GetUser(ctx, &model.GetAdminUserRequest{ID: user.ID})
}

func (c *AdminUseCase) SetUserStatus(ctx context.Context, request *model.SetAdminUserStatusRequest) (*model.SuspendUserResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	user := new(entity.User)
	if err := c.Users.FindById(tx, user, request.ID); err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "user not found")
	}

	user.Status = request.Status
	if err := c.Users.Update(tx, user); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	var paused int64
	if request.Status == entity.UserStatusSuspended {
		// suspending has to stop the work too, or their watchers keep firing
		count, err := c.Watchers.PauseAllForUser(tx, user.ID)
		if err != nil {
			return nil, fiber.ErrInternalServerError
		}
		paused = count
	}

	c.Audit.Record(ctx, tx, AuditEntry{ActorID: &request.ActorID, Action: "admin.user." + request.Status,
		EntityType: "users", EntityID: &user.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	if request.Status == entity.UserStatusSuspended {
		c.UserUC.revokeAllSessions(ctx, user.ID)
	}

	return &model.SuspendUserResponse{Status: user.Status, WatchersPaused: paused}, nil
}

// Impersonate issues a short-lived session as the target user. Every action
// taken on it is audit-logged with impersonated_user_id set, which is what
// makes offering this defensible at all.
func (c *AdminUseCase) Impersonate(ctx context.Context, request *model.ImpersonateRequest) (*model.ImpersonateResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	if request.ActorID == request.ID {
		return nil, fiber.NewError(fiber.StatusBadRequest, "you are already signed in as that user")
	}

	// Impersonating onwards from an impersonated session would make the audit
	// trail name the wrong admin: the session only records one impersonator, so
	// a chain collapses to whoever was impersonated last.
	if auth := model.AuthFromContext(ctx); auth != nil && auth.Impersonated {
		return nil, fiber.NewError(fiber.StatusForbidden,
			"end the current impersonation session before starting another")
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	user := new(entity.User)
	if err := c.Users.FindById(tx, user, request.ID); err != nil {
		return nil, fiber.NewError(fiber.StatusNotFound, "user not found")
	}

	if err := c.Users.LoadRoles(tx, user); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	token, session, err := c.UserUC.issueSession(tx, user, request.UserAgent, request.IP)
	if err != nil {
		return nil, err
	}

	// impersonation sessions are deliberately short, and are marked so that
	// Verify can tell them apart from the target user's own logins — without
	// the mark every action taken here is attributed to the target
	session.ExpiresAt = time.Now().Add(30 * time.Minute).UnixMilli()
	session.ImpersonatedBy = &request.ActorID
	if err := c.Sessions.Update(tx, session); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	c.Audit.Record(ctx, tx, AuditEntry{
		ActorID:            &request.ActorID,
		ImpersonatedUserID: &user.ID,
		Action:             "admin.user.impersonated",
		EntityType:         "users",
		EntityID:           &user.ID,
		IP:                 nilIfEmpty(request.IP),
	})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return &model.ImpersonateResponse{
		Token:         token,
		ExpiresAt:     session.ExpiresAt,
		Impersonating: converter.UserToResponse(user),
	}, nil
}

func (c *AdminUseCase) Stats(ctx context.Context) (*model.AdminStatsResponse, error) {
	db := c.DB.WithContext(ctx)
	now := time.Now()

	userCounts, err := c.Users.CountByStatus(db)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	watcherCounts, err := c.Watchers.CountByStatus(db)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	accountCounts, err := c.Accounts.CountByStatus(db)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	pending, overdue, oldest, err := c.Runs.QueueDepth(db, now.UnixMilli())
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	runCounts, err := c.Runs.CountByStatusSince(db, now.Add(-24*time.Hour).UnixMilli())
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return &model.AdminStatsResponse{
		Users:             model.StatusCounts(userCounts),
		WatchersActive:    watcherCounts[entity.WatcherStatusActive],
		MailAccountsError: accountCounts[entity.MailAccountStatusError],
		Queue: model.QueueStats{
			PendingRuns:          pending,
			OverdueRuns:          overdue,
			OldestPendingSeconds: oldest,
		},
		Runs24h: model.StatusCounts(runCounts),
	}, nil
}
