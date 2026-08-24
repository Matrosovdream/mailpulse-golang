package oauth

import (
	"context"
	"errors"

	"golang.org/x/oauth2"
)

// NewStub points a real OAuth client at a server we control.
//
// It exists for the same reason MAIL_STUB_ENABLED does: the flow cannot
// otherwise be exercised without a registered application at Google or
// Microsoft, and those take weeks. Everything below the endpoints is the
// production code path — the same exchange, the same refresh, the same
// merge-and-store — so what the tests cover is the real implementation and not
// a parallel one written to pass.
//
// It is off unless OAUTH_STUB_URL is set, and the server logs loudly when it
// is on.
func NewStub(slug, baseURL string) Client {
	if baseURL == "" {
		return nil
	}

	return &client{
		slug: slug,
		config: &oauth2.Config{
			ClientID:     "stub-client-id",
			ClientSecret: "stub-client-secret",
			RedirectURL:  CallbackURL(baseURL, slug),
			Scopes:       []string{"mail:imap_ro"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  baseURL + "/authorize",
				TokenURL: baseURL + "/token",
			},
		},
		identify: stubIdentify(baseURL),
	}
}

func stubIdentify(baseURL string) identifier {
	return func(ctx context.Context, tokens Tokens) (Identity, error) {
		var info struct {
			Subject string `json:"sub"`
			Email   string `json:"email"`
		}

		if err := getJSON(ctx, baseURL+"/userinfo", "Bearer "+tokens.AccessToken, &info); err != nil {
			return Identity{}, err
		}

		if info.Email == "" {
			return Identity{}, errors.New("oauth: the stub returned no email address")
		}

		return Identity{Email: info.Email, AccountID: info.Subject}, nil
	}
}
