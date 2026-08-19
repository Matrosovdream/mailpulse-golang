package entity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMailProvider_AuthModeList(t *testing.T) {
	cases := map[string][]string{
		"password,app_password":  {"password", "app_password"},
		" password , oauth2 ":    {"password", "oauth2"},
		"app_password":           {"app_password"},
		"":                       nil,
		"password,,app_password": {"password", "app_password"},
	}

	for stored, want := range cases {
		provider := MailProvider{AuthModes: stored}
		assert.Equal(t, want, provider.AuthModeList(), "auth_modes=%q", stored)
	}
}

func TestMailProvider_SupportsAuthMode(t *testing.T) {
	provider := MailProvider{AuthModes: "app_password,xoauth2"}

	assert.True(t, provider.SupportsAuthMode("app_password"))
	assert.True(t, provider.SupportsAuthMode("xoauth2"))
	assert.False(t, provider.SupportsAuthMode("password"),
		"Yandex rejecting a plain password is the case this guards")
	assert.False(t, provider.SupportsAuthMode(""))
}

func TestUser_Roles(t *testing.T) {
	user := User{Roles: []Role{{Slug: RoleUser}, {Slug: RoleSuperadmin}}}

	assert.Equal(t, []string{RoleUser, RoleSuperadmin}, user.RoleSlugs())
	assert.True(t, user.HasRole(RoleSuperadmin))
	assert.False(t, user.HasRole("nonsense"))

	empty := User{}
	assert.Empty(t, empty.RoleSlugs())
	assert.False(t, empty.HasRole(RoleUser))
}
