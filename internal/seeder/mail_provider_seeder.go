package seeder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"mailpulse/internal/entity"

	"gorm.io/gorm"
)

// MailProviderSeed is one row of db/seeds/mail_providers.json.
//
// The initial providers ship in a migration so a fresh database is usable
// immediately. This seeder exists so a new integration, or a changed preset,
// is a file edit rather than a migration — which was the point of making
// providers data in the first place.
type MailProviderSeed struct {
	Slug          string  `json:"slug"`
	Label         string  `json:"label"`
	Kind          string  `json:"kind"`
	AuthModes     string  `json:"auth_modes"`
	DefaultHost   *string `json:"default_host"`
	DefaultPort   *int    `json:"default_port"`
	DefaultUseTLS *bool   `json:"default_use_tls"`
	HelpURL       *string `json:"help_url"`
	Enabled       *bool   `json:"enabled"`
	Position      int     `json:"position"`
}

type MailProviderSeeder struct{}

func NewMailProviderSeeder() *MailProviderSeeder { return &MailProviderSeeder{} }

func (s *MailProviderSeeder) Name() string { return "mail_providers" }

func (s *MailProviderSeeder) Seed(ctx context.Context, db *gorm.DB, raw json.RawMessage) (Result, error) {
	var seeds []MailProviderSeed
	if err := json.Unmarshal(raw, &seeds); err != nil {
		return Result{}, fmt.Errorf("mail_providers.json must be a list of providers: %w", err)
	}

	var result Result

	for i := range seeds {
		seed := seeds[i]

		if seed.Slug == "" || seed.Kind == "" || seed.AuthModes == "" {
			return result, fmt.Errorf("provider %q needs a slug, a kind and auth_modes", seed.Slug)
		}

		existing := new(entity.MailProvider)
		err := db.Where("slug = ?", seed.Slug).Take(existing).Error

		row := entity.MailProvider{
			Slug:          seed.Slug,
			Label:         orDefault(seed.Label, seed.Slug),
			Kind:          seed.Kind,
			AuthModes:     seed.AuthModes,
			DefaultHost:   seed.DefaultHost,
			DefaultPort:   seed.DefaultPort,
			DefaultUseTLS: boolOrDefault(seed.DefaultUseTLS, true),
			HelpURL:       seed.HelpURL,
			Enabled:       boolOrDefault(seed.Enabled, true),
			Position:      seed.Position,
		}

		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if createErr := db.Create(&row).Error; createErr != nil {
				return result, createErr
			}
			result.Created++

		case err != nil:
			return result, err

		default:
			if sameProvider(existing, &row) {
				result.Skipped++
				continue
			}

			// Slug is the primary key, so Updates rather than Save: the
			// created_at of an existing row is not the seeder's to rewrite.
			if updateErr := db.Model(&entity.MailProvider{}).
				Where("slug = ?", seed.Slug).
				Updates(map[string]any{
					"label":           row.Label,
					"kind":            row.Kind,
					"auth_modes":      row.AuthModes,
					"default_host":    row.DefaultHost,
					"default_port":    row.DefaultPort,
					"default_use_tls": row.DefaultUseTLS,
					"help_url":        row.HelpURL,
					"enabled":         row.Enabled,
					"position":        row.Position,
				}).Error; updateErr != nil {
				return result, updateErr
			}
			result.Updated++
		}
	}

	return result, nil
}

func sameProvider(a, b *entity.MailProvider) bool {
	return a.Label == b.Label &&
		a.Kind == b.Kind &&
		a.AuthModes == b.AuthModes &&
		samePtr(a.DefaultHost, b.DefaultHost) &&
		samePtrInt(a.DefaultPort, b.DefaultPort) &&
		a.DefaultUseTLS == b.DefaultUseTLS &&
		samePtr(a.HelpURL, b.HelpURL) &&
		a.Enabled == b.Enabled &&
		a.Position == b.Position
}

func samePtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func samePtrInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func boolOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
