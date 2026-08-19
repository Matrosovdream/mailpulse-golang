package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"mailpulse/internal/entity"
	"mailpulse/internal/gateway/cache"
	"mailpulse/internal/gateway/messaging"
	"mailpulse/internal/model"
	"mailpulse/internal/model/converter"
	"mailpulse/internal/repository"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type UserUseCase struct {
	DB         *gorm.DB
	Log        *logrus.Logger
	Validate   *validator.Validate
	Users      *repository.UserRepository
	Roles      *repository.RoleRepository
	Sessions   *repository.UserSessionRepository
	Audit      *AuditUseCase
	Producer   *messaging.UserProducer
	UserCache  *cache.UserCache
	ResetCache *cache.PasswordResetCache
	SessionTTL time.Duration
}

func NewUserUseCase(db *gorm.DB, log *logrus.Logger, validate *validator.Validate,
	users *repository.UserRepository, roles *repository.RoleRepository,
	sessions *repository.UserSessionRepository, audit *AuditUseCase,
	producer *messaging.UserProducer, userCache *cache.UserCache,
	resetCache *cache.PasswordResetCache, sessionTTL time.Duration) *UserUseCase {
	return &UserUseCase{
		DB: db, Log: log, Validate: validate,
		Users: users, Roles: roles, Sessions: sessions, Audit: audit,
		Producer: producer, UserCache: userCache, ResetCache: resetCache,
		SessionTTL: sessionTTL,
	}
}

// hashToken keeps the bearer token out of the database: a dump gives an
// attacker hashes, not usable sessions.
//
// The redis cache is keyed by the same hash. That is what makes revocation
// possible at all — a session row stores only the hash, so keying the cache by
// the raw token left no way to find the entry to evict. It also stops a redis
// dump from handing over usable bearer tokens as key names.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Verify resolves a bearer token to an Auth, reading through the redis cache.
func (c *UserUseCase) Verify(ctx context.Context, request *model.VerifyUserRequest) (*model.Auth, error) {
	if err := c.Validate.Struct(request); err != nil {
		c.Log.Warnf("Invalid verify request : %+v", err)
		return nil, fiber.ErrUnauthorized
	}

	tokenHash := hashToken(request.Token)

	if auth, err := c.UserCache.Get(ctx, tokenHash); err == nil && auth != nil {
		return auth, nil
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	now := time.Now().UnixMilli()

	session := new(entity.UserSession)
	if err := c.Sessions.FindActiveByTokenHash(tx, session, tokenHash, now); err != nil {
		c.Log.Warnf("No active session for token : %+v", err)
		return nil, fiber.ErrUnauthorized
	}

	user := new(entity.User)
	if err := c.Users.FindById(tx, user, session.UserID); err != nil {
		c.Log.Warnf("Session points at a missing user : %+v", err)
		return nil, fiber.ErrUnauthorized
	}

	if user.Status != entity.UserStatusActive {
		c.Log.Warnf("Rejecting %s user %s", user.Status, user.ID)
		return nil, fiber.ErrForbidden
	}

	if err := c.Users.LoadRoles(tx, user); err != nil {
		c.Log.Warnf("Failed load roles : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	// written outside the read path's critical section; a stale last_used_at
	// is not worth a write on every single request
	_ = c.Sessions.TouchLastUsed(tx, session.ID, now)

	if err := tx.Commit().Error; err != nil {
		c.Log.Warnf("Failed commit transaction : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	auth := converter.UserToAuth(user, session.ID, session.ImpersonatedBy)
	if err := c.UserCache.Set(ctx, tokenHash, auth); err != nil {
		c.Log.Warnf("Failed cache auth token : %+v", err)
	}

	return auth, nil
}

func (c *UserUseCase) Create(ctx context.Context, request *model.RegisterUserRequest) (*model.UserResponse, error) {
	// normalise before validating: a pasted address often carries surrounding
	// whitespace, and the email rule would reject it before the trim below
	// ever ran
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))

	if err := c.Validate.Struct(request); err != nil {
		c.Log.Warnf("Invalid register request : %+v", err)
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	email := request.Email

	total, err := c.Users.CountByEmail(tx, email)
	if err != nil {
		c.Log.Warnf("Failed count users : %+v", err)
		return nil, fiber.ErrInternalServerError
	}
	if total > 0 {
		return nil, fiber.NewError(fiber.StatusConflict, "that email is already registered")
	}

	password, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		c.Log.Warnf("Failed to hash password : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	timezone := request.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	user := &entity.User{
		ID:       uuid.NewString(),
		Email:    email,
		Name:     request.Name,
		Password: string(password),
		Status:   entity.UserStatusActive,
		Timezone: timezone,
	}

	if err := c.Users.Create(tx, user); err != nil {
		c.Log.Warnf("Failed create user : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	// every account starts with the plain user role; superadmin is only ever
	// granted through the admin endpoint
	role := new(entity.Role)
	if err := c.Roles.FindBySlug(tx, role, entity.RoleUser); err != nil {
		c.Log.Warnf("Seeded role %q is missing : %+v", entity.RoleUser, err)
		return nil, fiber.ErrInternalServerError
	}
	if err := c.Users.AddRole(tx, user.ID, role.ID); err != nil {
		c.Log.Warnf("Failed assign default role : %+v", err)
		return nil, fiber.ErrInternalServerError
	}
	user.Roles = []entity.Role{*role}

	c.Audit.Record(ctx, tx, AuditEntry{ActorID: &user.ID, Action: "user.registered", EntityType: "users", EntityID: &user.ID})

	if err := tx.Commit().Error; err != nil {
		c.Log.Warnf("Failed commit transaction : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	c.publish(user, "user created")

	return converter.UserToResponse(user), nil
}

func (c *UserUseCase) Login(ctx context.Context, request *model.LoginUserRequest) (*model.LoginResponse, error) {
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))

	if err := c.Validate.Struct(request); err != nil {
		c.Log.Warnf("Invalid login request : %+v", err)
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	user := new(entity.User)
	if err := c.Users.FindByEmail(tx, user, request.Email); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// same error and roughly the same cost as a wrong password, so the
			// response does not reveal which addresses are registered
			bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidinvalidin"), []byte(request.Password))
			return nil, fiber.ErrUnauthorized
		}
		c.Log.Warnf("Failed find user by email : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(request.Password)); err != nil {
		return nil, fiber.ErrUnauthorized
	}

	if user.Status != entity.UserStatusActive {
		return nil, fiber.NewError(fiber.StatusForbidden, "this account is suspended")
	}

	if err := c.Users.LoadRoles(tx, user); err != nil {
		c.Log.Warnf("Failed load roles : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	token, session, err := c.issueSession(tx, user, request.UserAgent, request.IP)
	if err != nil {
		return nil, err
	}

	c.Audit.Record(ctx, tx, AuditEntry{ActorID: &user.ID, Action: "user.logged_in", EntityType: "user_sessions", EntityID: &session.ID, IP: nilIfEmpty(request.IP)})

	if err := tx.Commit().Error; err != nil {
		c.Log.Warnf("Failed commit transaction : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	c.publish(user, "user login")

	return &model.LoginResponse{
		Token:     token,
		ExpiresAt: session.ExpiresAt,
		User:      converter.UserToResponse(user),
	}, nil
}

func (c *UserUseCase) issueSession(tx *gorm.DB, user *entity.User, userAgent, ip string) (string, *entity.UserSession, error) {
	token := uuid.NewString() + uuid.NewString()
	now := time.Now()

	session := &entity.UserSession{
		ID:         uuid.NewString(),
		UserID:     user.ID,
		TokenHash:  hashToken(token),
		UserAgent:  nilIfEmpty(truncate(userAgent, 255)),
		IP:         nilIfEmpty(ip),
		ExpiresAt:  now.Add(c.SessionTTL).UnixMilli(),
		LastUsedAt: now.UnixMilli(),
	}

	if err := c.Sessions.Create(tx, session); err != nil {
		c.Log.Warnf("Failed create session : %+v", err)
		return "", nil, fiber.ErrInternalServerError
	}

	return token, session, nil
}

func (c *UserUseCase) Current(ctx context.Context, request *model.GetUserRequest) (*model.UserResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	user := new(entity.User)
	if err := c.Users.FindById(tx, user, request.ID); err != nil {
		return nil, fiber.ErrNotFound
	}

	if err := c.Users.LoadRoles(tx, user); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	return converter.UserToResponse(user), nil
}

func (c *UserUseCase) Update(ctx context.Context, request *model.UpdateUserRequest) (*model.UserResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		c.Log.Warnf("Invalid update request : %+v", err)
		return nil, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	user := new(entity.User)
	if err := c.Users.FindById(tx, user, request.ID); err != nil {
		return nil, fiber.ErrNotFound
	}

	if request.Name != "" {
		user.Name = request.Name
	}
	if request.Timezone != "" {
		user.Timezone = request.Timezone
	}
	if request.Password != "" {
		password, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fiber.ErrInternalServerError
		}
		user.Password = string(password)
	}

	if err := c.Users.Update(tx, user); err != nil {
		c.Log.Warnf("Failed save user : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	if err := c.Users.LoadRoles(tx, user); err != nil {
		return nil, fiber.ErrInternalServerError
	}

	c.Audit.Record(ctx, tx, AuditEntry{ActorID: &user.ID, Action: "user.updated", EntityType: "users", EntityID: &user.ID})

	if err := tx.Commit().Error; err != nil {
		return nil, fiber.ErrInternalServerError
	}

	// a password change invalidates every session, including this one
	if request.Password != "" {
		c.revokeAllSessions(ctx, user.ID)
	}

	c.publish(user, "user updated")

	return converter.UserToResponse(user), nil
}

func (c *UserUseCase) Logout(ctx context.Context, request *model.LogoutUserRequest) (bool, error) {
	if err := c.Validate.Struct(request); err != nil {
		return false, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	if err := c.Sessions.Revoke(tx, request.SessionID, time.Now().UnixMilli()); err != nil {
		c.Log.Warnf("Failed revoke session : %+v", err)
		return false, fiber.ErrInternalServerError
	}

	if err := tx.Commit().Error; err != nil {
		return false, fiber.ErrInternalServerError
	}

	// without this the revoked token keeps authenticating until the TTL lapses
	if request.Token != "" {
		if err := c.UserCache.Delete(ctx, hashToken(request.Token)); err != nil {
			c.Log.Warnf("Failed evict auth token : %+v", err)
		}
	}

	return true, nil
}

func (c *UserUseCase) ListSessions(ctx context.Context, request *model.ListSessionRequest) ([]model.SessionResponse, error) {
	if err := c.Validate.Struct(request); err != nil {
		return nil, fiber.ErrBadRequest
	}

	sessions, err := c.Sessions.FindActiveByUser(c.DB.WithContext(ctx), request.UserID, time.Now().UnixMilli())
	if err != nil {
		c.Log.Warnf("Failed list sessions : %+v", err)
		return nil, fiber.ErrInternalServerError
	}

	return converter.SessionsToResponses(sessions, request.CurrentSessionID), nil
}

func (c *UserUseCase) RevokeSession(ctx context.Context, request *model.RevokeSessionRequest) (bool, error) {
	if err := c.Validate.Struct(request); err != nil {
		return false, fiber.ErrBadRequest
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	session := new(entity.UserSession)
	if err := c.Sessions.FindByIdAndUser(tx, session, request.SessionID, request.UserID); err != nil {
		return false, fiber.ErrNotFound
	}

	if err := c.Sessions.Revoke(tx, session.ID, time.Now().UnixMilli()); err != nil {
		return false, fiber.ErrInternalServerError
	}

	if err := tx.Commit().Error; err != nil {
		return false, fiber.ErrInternalServerError
	}

	// the session row carries the same hash the cache is keyed by, so revoking
	// another device takes effect immediately rather than at the end of the TTL
	if err := c.UserCache.Delete(ctx, session.TokenHash); err != nil {
		c.Log.Warnf("Failed evict revoked session %s : %+v", session.ID, err)
	}

	return true, nil
}

func (c *UserUseCase) ForgotPassword(ctx context.Context, request *model.ForgotPasswordRequest) (bool, error) {
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))

	if err := c.Validate.Struct(request); err != nil {
		return false, fiber.ErrBadRequest
	}

	user := new(entity.User)
	err := c.Users.FindByEmail(c.DB.WithContext(ctx), user, request.Email)
	if err != nil {
		// always report success: whether an address is registered is not
		// something an unauthenticated caller should be able to probe
		c.Log.Infof("Password reset requested for unknown address")
		return true, nil
	}

	token := uuid.NewString() + uuid.NewString()
	if err := c.ResetCache.Set(ctx, token, &model.PasswordReset{UserID: user.ID, Email: user.Email}); err != nil {
		c.Log.Warnf("Failed store reset token : %+v", err)
		return false, fiber.ErrInternalServerError
	}

	// delivery belongs to a notifier channel or a transactional mail provider;
	// until one is wired the token is only logged
	c.Log.Infof("Password reset token for %s: %s", user.Email, token)

	return true, nil
}

func (c *UserUseCase) ResetPassword(ctx context.Context, request *model.ResetPasswordRequest) (bool, error) {
	if err := c.Validate.Struct(request); err != nil {
		return false, fiber.ErrBadRequest
	}

	reset, err := c.ResetCache.Get(ctx, request.Token)
	if err != nil || reset == nil {
		return false, fiber.NewError(fiber.StatusBadRequest, "that reset link is invalid or has expired")
	}

	tx := c.DB.WithContext(ctx).Begin()
	defer tx.Rollback()

	user := new(entity.User)
	if err := c.Users.FindById(tx, user, reset.UserID); err != nil {
		return false, fiber.ErrNotFound
	}

	password, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		return false, fiber.ErrInternalServerError
	}
	user.Password = string(password)

	if err := c.Users.Update(tx, user); err != nil {
		return false, fiber.ErrInternalServerError
	}

	c.Audit.Record(ctx, tx, AuditEntry{ActorID: &user.ID, Action: "user.password_reset", EntityType: "users", EntityID: &user.ID})

	if err := tx.Commit().Error; err != nil {
		return false, fiber.ErrInternalServerError
	}

	// single use
	_ = c.ResetCache.Delete(ctx, request.Token)
	c.revokeAllSessions(ctx, user.ID)

	return true, nil
}

// revokeAllSessions is used whenever a credential changes. The returned hashes
// are the cache keys: revoking in the database alone would leave every one of
// those sessions authenticating from cache until REDIS_TTL_AUTH lapsed.
func (c *UserUseCase) revokeAllSessions(ctx context.Context, userID string) {
	hashes, err := c.Sessions.RevokeAllForUser(c.DB.WithContext(ctx), userID, time.Now().UnixMilli())
	if err != nil {
		c.Log.Warnf("Failed revoke sessions for %s : %+v", userID, err)
		return
	}

	if err := c.UserCache.Delete(ctx, hashes...); err != nil {
		c.Log.Warnf("Failed evict sessions for %s : %+v", userID, err)
	}
}

func (c *UserUseCase) publish(user *entity.User, what string) {
	if c.Producer == nil {
		c.Log.Debugf("Kafka producer is disabled, skipping %s event", what)
		return
	}

	if err := c.Producer.Send(converter.UserToEvent(user)); err != nil {
		c.Log.Warnf("Failed publish %s event : %+v", what, err)
	}
}
