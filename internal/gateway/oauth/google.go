package oauth

import (
	"context"
	"errors"

	"golang.org/x/oauth2"
)

// GoogleSlug is the mail_providers row this client serves.
const GoogleSlug = "gmail"

// Google scopes.
//
// The mail scope is https://mail.google.com/ and not gmail.readonly, which is
// the obvious-looking choice and does not work: gmail.readonly grants the Gmail
// API only, and IMAP with XOAUTH2 is refused with AUTHENTICATIONFAILED. Both
// are restricted scopes needing the same security assessment, so the narrower
// one buys nothing here — it just fails later. Revisit if the Gmail API
// provider lands and IMAP is dropped for Google.
const googleMailScope = "https://mail.google.com/"

const googleUserInfoURL = "https://www.googleapis.com/oauth2/v3/userinfo"

// NewGoogle returns nil when the environment did not configure Google, so
// Registry.Register can be handed every provider unconditionally.
func NewGoogle(config Config, callbackBase string) Client {
	if !config.Configured() {
		return nil
	}

	return &client{
		slug: GoogleSlug,
		config: &oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			RedirectURL:  CallbackURL(callbackBase, GoogleSlug),
			Scopes:       []string{googleMailScope, "openid", "email"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
				TokenURL: "https://oauth2.googleapis.com/token",
			},
		},
		// Without access_type=offline Google issues no refresh token at all,
		// and without prompt=consent it issues one only on the very first
		// consent — so a user who reconnects gets an account that works for an
		// hour and then stops. Both are required, not belt and braces.
		authParams: []oauth2.AuthCodeOption{
			oauth2.AccessTypeOffline,
			oauth2.SetAuthURLParam("prompt", "consent"),
		},
		identify: googleIdentify,
	}
}

func googleIdentify(ctx context.Context, tokens Tokens) (Identity, error) {
	var info struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
	}

	if err := getJSON(ctx, googleUserInfoURL, "Bearer "+tokens.AccessToken, &info); err != nil {
		return Identity{}, err
	}

	if info.Email == "" {
		return Identity{}, errors.New("oauth: Google returned no email address, the email scope was not granted")
	}

	return Identity{Email: info.Email, AccountID: info.Subject}, nil
}
