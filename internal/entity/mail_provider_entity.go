package entity

import "strings"

// MailProvider is a connectable mail service. It is a table rather than a
// check constraint so adding Fastmail or Zoho is a seed row: Kind names the
// client implementation in internal/gateway/mail that serves it, so many
// providers share one client.
type MailProvider struct {
	Slug          string  `gorm:"column:slug;primaryKey"`
	Label         string  `gorm:"column:label"`
	Kind          string  `gorm:"column:kind"`
	AuthModes     string  `gorm:"column:auth_modes"`
	DefaultHost   *string `gorm:"column:default_host"`
	DefaultPort   *int    `gorm:"column:default_port"`
	DefaultUseTLS bool    `gorm:"column:default_use_tls"`
	HelpURL       *string `gorm:"column:help_url"`
	Enabled       bool    `gorm:"column:enabled"`
	Position      int     `gorm:"column:position"`
	CreatedAt     int64   `gorm:"column:created_at;autoCreateTime:milli"`
	UpdatedAt     int64   `gorm:"column:updated_at;autoCreateTime:milli;autoUpdateTime:milli"`
}

func (p *MailProvider) TableName() string {
	return "mail_providers"
}

// AuthModeList splits the stored csv.
func (p *MailProvider) AuthModeList() []string {
	if p.AuthModes == "" {
		return nil
	}

	parts := strings.Split(p.AuthModes, ",")
	modes := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			modes = append(modes, trimmed)
		}
	}
	return modes
}

func (p *MailProvider) SupportsAuthMode(mode string) bool {
	for _, candidate := range p.AuthModeList() {
		if candidate == mode {
			return true
		}
	}
	return false
}
