package seeder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"mailpulse/internal/entity"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserSeed is one row of db/seeds/users.json.
//
// Password is plaintext in the file and hashed on the way in — these are
// bootstrap accounts, so the file itself is the secret and must not hold
// anything real.
type UserSeed struct {
	Email    string   `json:"email"`
	Name     string   `json:"name"`
	Password string   `json:"password"`
	Status   string   `json:"status"`
	Timezone string   `json:"timezone"`
	Roles    []string `json:"roles"`
}

type UserSeeder struct{}

func NewUserSeeder() *UserSeeder { return &UserSeeder{} }

func (s *UserSeeder) Name() string { return "users" }

func (s *UserSeeder) Seed(ctx context.Context, db *gorm.DB, raw json.RawMessage) (Result, error) {
	var seeds []UserSeed
	if err := json.Unmarshal(raw, &seeds); err != nil {
		return Result{}, fmt.Errorf("users.json must be a list of users: %w", err)
	}

	var result Result

	for i := range seeds {
		seed := seeds[i]

		email := strings.ToLower(strings.TrimSpace(seed.Email))
		if email == "" || seed.Password == "" {
			return result, fmt.Errorf("every user needs an email and a password")
		}

		existing := new(entity.User)
		err := db.Where("email = ?", email).Take(existing).Error

		switch {
		case err == nil:
			// An existing account is left alone apart from its roles: seeding
			// must never reset a password someone has since changed.
			changed, roleErr := s.syncRoles(db, existing.ID, seed.Roles)
			if roleErr != nil {
				return result, roleErr
			}
			if changed {
				result.Updated++
			} else {
				result.Skipped++
			}

		case errors.Is(err, gorm.ErrRecordNotFound):
			hashed, hashErr := bcrypt.GenerateFromPassword([]byte(seed.Password), bcrypt.DefaultCost)
			if hashErr != nil {
				return result, hashErr
			}

			user := &entity.User{
				ID:       uuid.NewString(),
				Email:    email,
				Name:     orDefault(seed.Name, email),
				Password: string(hashed),
				Status:   orDefault(seed.Status, entity.UserStatusActive),
				Timezone: orDefault(seed.Timezone, "UTC"),
			}

			// The SELECT above is a hint, not a lock: two seeder processes can
			// both reach here for the same address. Letting the unique index
			// decide makes the loser a no-op instead of a crash.
			insert := db.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "email"}},
				DoNothing: true,
			}).Create(user)
			if insert.Error != nil {
				return result, insert.Error
			}

			if insert.RowsAffected == 0 {
				// someone else won the race; adopt their row so the roles below
				// attach to the user that actually exists
				if findErr := db.Where("email = ?", email).Take(existing).Error; findErr != nil {
					return result, findErr
				}
				user.ID = existing.ID
				result.Skipped++
			} else {
				result.Created++
			}

			if _, roleErr := s.syncRoles(db, user.ID, seed.Roles); roleErr != nil {
				return result, roleErr
			}

		default:
			return result, err
		}
	}

	return result, nil
}

// syncRoles adds any missing role links and reports whether it changed
// anything. It only adds: a role granted by hand in the admin UI is not
// something a re-seed should take away.
func (s *UserSeeder) syncRoles(db *gorm.DB, userID string, slugs []string) (bool, error) {
	if len(slugs) == 0 {
		slugs = []string{entity.RoleUser}
	}

	changed := false

	for _, slug := range slugs {
		role := new(entity.Role)
		if err := db.Where("slug = ?", slug).Take(role).Error; err != nil {
			return changed, fmt.Errorf("role %q does not exist, run the migrations first", slug)
		}

		var linked int64
		if err := db.Model(&entity.UserRole{}).
			Where("user_id = ? AND role_id = ?", userID, role.ID).
			Count(&linked).Error; err != nil {
			return changed, err
		}
		if linked > 0 {
			continue
		}

		// same reasoning as the user insert: the composite primary key is the
		// real guard, the count above is only an optimisation
		link := db.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&entity.UserRole{UserID: userID, RoleID: role.ID})
		if link.Error != nil {
			return changed, link.Error
		}
		if link.RowsAffected > 0 {
			changed = true
		}
	}

	return changed, nil
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
