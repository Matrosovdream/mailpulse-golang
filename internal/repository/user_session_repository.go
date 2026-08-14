package repository

import (
	"mailpulse/internal/entity"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type UserSessionRepository struct {
	Repository[entity.UserSession]
	Log *logrus.Logger
}

func NewUserSessionRepository(log *logrus.Logger) *UserSessionRepository {
	return &UserSessionRepository{Log: log}
}

// FindActiveByTokenHash is the hot path behind every authenticated request,
// which is why the Redis cache sits in front of it.
func (r *UserSessionRepository) FindActiveByTokenHash(db *gorm.DB, session *entity.UserSession, hash string, now int64) error {
	return db.Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", hash, now).Take(session).Error
}

func (r *UserSessionRepository) FindActiveByUser(db *gorm.DB, userID string, now int64) ([]entity.UserSession, error) {
	var sessions []entity.UserSession
	err := db.Where("user_id = ? AND revoked_at IS NULL AND expires_at > ?", userID, now).
		Order("last_used_at DESC").
		Find(&sessions).Error
	return sessions, err
}

func (r *UserSessionRepository) Revoke(db *gorm.DB, id string, now int64) error {
	return db.Model(&entity.UserSession{}).
		Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", now).Error
}

// RevokeAllForUser returns the token hashes it revoked so the caller can evict
// them from the cache; otherwise a revoked session keeps authenticating until
// its TTL lapses.
func (r *UserSessionRepository) RevokeAllForUser(db *gorm.DB, userID string, now int64) ([]string, error) {
	var sessions []entity.UserSession
	if err := db.Where("user_id = ? AND revoked_at IS NULL", userID).Find(&sessions).Error; err != nil {
		return nil, err
	}

	if len(sessions) == 0 {
		return nil, nil
	}

	if err := db.Model(&entity.UserSession{}).
		Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", now).Error; err != nil {
		return nil, err
	}

	hashes := make([]string, 0, len(sessions))
	for _, session := range sessions {
		hashes = append(hashes, session.TokenHash)
	}
	return hashes, nil
}

func (r *UserSessionRepository) TouchLastUsed(db *gorm.DB, id string, now int64) error {
	return db.Model(&entity.UserSession{}).Where("id = ?", id).Update("last_used_at", now).Error
}
