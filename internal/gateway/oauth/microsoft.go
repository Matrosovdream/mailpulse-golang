package oauth

import (
	"context"
	"errors"

	"golang.org/x/oauth2"
)

// MicrosoftSlug is the mail_providers row this client serves.
const MicrosoftSlug = "outlook"

// microsoftMailScope is the delegated permission that lets an app open the
// user's mailbox over IMAP.
const microsoftMailScope = "https://outlook.office.com/IMAP.AccessAsUser.All"

// NewMicrosoft returns nil when the environment did not configure Microsoft.
//
// Tenant defaults to "common", which admits both work/school and personal
// accounts. A single-tenant app registration must set its directory id here or
// its own users are turned away.
func NewMicrosoft(config Config, callbackBase string) Client {
	if !config.Configured() {
		return nil
	}

	tenant := config.Tenant
	if tenant == "" {
		tenant = "common"
	}

	base := "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0"

	return &client{
		slug: MicrosoftSlug,
		config: &oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			RedirectURL:  CallbackURL(callbackBase, MicrosoftSlug),
			// offline_access is how v2.0 asks for a refresh token; openid and
			// email are what make the id_token identity below possible.
			Scopes: []string{microsoftMailScope, "offline_access", "openid", "email", "profile"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  base + "/authorize",
				TokenURL: base + "/token",
			},
		},
		identify: microsoftIdentify,
	}
}

// microsoftIdentify reads the mailbox from the id_token rather than calling
// Microsoft Graph.
//
// Graph /me is the obvious choice and does not work: an Azure v2.0 access token
// is addressed to exactly one resource, and ours is addressed to
// outlook.office.com, so graph.microsoft.com answers 401. The Outlook REST API
// that would have accepted it was retired in 2022. The id_token comes back from
// the same token response and carries the claim we need.
func microsoftIdentify(ctx context.Context, tokens Tokens) (Identity, error) {
	if tokens.IDToken == "" {
		return Identity{}, errors.New("oauth: Microsoft returned no id_token, the openid scope was not granted")
	}

	claims, err := idTokenClaims(tokens.IDToken)
	if err != nil {
		return Identity{}, err
	}

	// personal accounts fill email; work and school accounts often only fill
	// preferred_username, which holds the UPN and is the address IMAP wants
	email := claim(claims, "email", "preferred_username", "upn")
	if email == "" {
		return Identity{}, errors.New("oauth: Microsoft returned no email address in the id_token")
	}

	return Identity{Email: email, AccountID: claim(claims, "oid", "sub")}, nil
}
