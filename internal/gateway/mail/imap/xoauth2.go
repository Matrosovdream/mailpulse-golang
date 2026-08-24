package imap

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/emersion/go-sasl"
)

// XOAuth2 is the SASL mechanism name Google, Microsoft and Yandex all accept on
// IMAP for an OAuth access token.
const XOAuth2 = "XOAUTH2"

// xoauth2Client implements XOAUTH2, which go-sasl does not ship.
//
// The library has OAUTHBEARER (RFC 7628) and nothing else, and the two are not
// interchangeable: XOAUTH2 predates the RFC, uses a different mechanism name
// and a different initial response, and is the one every mail provider actually
// advertises on IMAP. Twenty lines here beats forking the dependency.
//
// The format is Google's:
//
//	user=<address>^Aauth=Bearer <token>^A^A
//
// where ^A is 0x01. go-imap base64-encodes the initial response for us.
type xoauth2Client struct {
	username string
	token    string
}

// NewXOAuth2 returns a SASL client that authenticates with an OAuth access
// token instead of a password.
func NewXOAuth2(username, token string) sasl.Client {
	return &xoauth2Client{username: username, token: token}
}

func (a *xoauth2Client) Start() (string, []byte, error) {
	if a.token == "" {
		return "", nil, errors.New("imap: cannot authenticate with an empty access token")
	}
	return XOAuth2, []byte("user=" + a.username + "\x01auth=Bearer " + a.token + "\x01\x01"), nil
}

// Next handles the failure path, which is the only time the server says
// anything back.
//
// On rejection the server sends a base64 JSON blob describing why and then
// waits for an empty line before failing the command. Returning an error here
// aborts the exchange instead, which go-imap turns into a clean failure and
// keeps the reason attached — without it the user is told only that
// authentication failed, and an expired token looks exactly like a revoked one.
func (a *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	detail := strings.TrimSpace(string(challenge))

	// the challenge is base64 inside the already-decoded SASL payload on some
	// servers and plain JSON on others; show whichever is readable
	if decoded, err := base64.StdEncoding.DecodeString(detail); err == nil {
		detail = strings.TrimSpace(string(decoded))
	}

	if detail == "" {
		return nil, errors.New("imap: the server rejected the access token")
	}

	return nil, fmt.Errorf("imap: the server rejected the access token: %s", detail)
}
