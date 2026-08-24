package oauth

import (
	"context"
	"errors"

	"golang.org/x/oauth2"
)

// YandexSlug is the mail_providers row this client serves.
const YandexSlug = "yandex"

const yandexUserInfoURL = "https://login.yandex.ru/info?format=json"

// NewYandex returns nil when the environment did not configure Yandex.
//
// Yandex is here even though the plan named only Google and Microsoft, because
// registering a Yandex OAuth app takes minutes and needs no security review.
// That makes it the only provider this flow can be proved against before
// Google's restricted-scope assessment clears.
func NewYandex(config Config, callbackBase string) Client {
	if !config.Configured() {
		return nil
	}

	return &client{
		slug: YandexSlug,
		config: &oauth2.Config{
			ClientID:     config.ClientID,
			ClientSecret: config.ClientSecret,
			RedirectURL:  CallbackURL(callbackBase, YandexSlug),
			// mail:imap_ro is read-only IMAP, which is all this application
			// ever does; login:email is what makes the mailbox identifiable.
			Scopes: []string{"mail:imap_ro", "login:email"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://oauth.yandex.ru/authorize",
				TokenURL: "https://oauth.yandex.ru/token",
			},
		},
		identify: yandexIdentify,
	}
}

// yandexIdentify calls the passport API, which authenticates with the "OAuth"
// scheme rather than "Bearer" — Yandex predates that convention and rejects
// Bearer outright.
func yandexIdentify(ctx context.Context, tokens Tokens) (Identity, error) {
	var info struct {
		ID           string `json:"id"`
		DefaultEmail string `json:"default_email"`
	}

	if err := getJSON(ctx, yandexUserInfoURL, "OAuth "+tokens.AccessToken, &info); err != nil {
		return Identity{}, err
	}

	if info.DefaultEmail == "" {
		return Identity{}, errors.New("oauth: Yandex returned no email address, the login:email scope was not granted")
	}

	return Identity{Email: info.DefaultEmail, AccountID: info.ID}, nil
}
