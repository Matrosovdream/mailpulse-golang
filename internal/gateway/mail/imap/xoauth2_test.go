package imap

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXOAuth2_InitialResponse(t *testing.T) {
	mechanism, response, err := NewXOAuth2("user@example.com", "ya29.token").Start()
	require.NoError(t, err)

	assert.Equal(t, "XOAUTH2", mechanism,
		"the mechanism name is what the server matched in its CAPABILITY list")

	// byte for byte: the separators are 0x01 and there are two of them at the
	// end, and getting either wrong is rejected by the server with a message
	// that says nothing about which part was malformed
	assert.Equal(t, "user=user@example.com\x01auth=Bearer ya29.token\x01\x01", string(response))
}

func TestXOAuth2_RefusesAnEmptyToken(t *testing.T) {
	_, _, err := NewXOAuth2("user@example.com", "").Start()

	// better to fail here than to send "auth=Bearer " and have the server
	// answer with a generic authentication failure
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty access token")
}

// The server only ever speaks on the failure path, and it expects an empty
// line back before it fails the command. Returning an error instead aborts the
// exchange immediately and keeps the reason, which is the difference between
// telling the user their token expired and telling them "authentication
// failed".
func TestXOAuth2_ChallengeAborts(t *testing.T) {
	challenge := base64.StdEncoding.EncodeToString(
		[]byte(`{"status":"400","schemes":"Bearer","scope":"https://mail.google.com/"}`))

	response, err := NewXOAuth2("user@example.com", "token").Next([]byte(challenge))

	require.Error(t, err)
	assert.Nil(t, response, "continuing the exchange would hang until the server gave up")
	assert.Contains(t, err.Error(), `"status":"400"`,
		"the server's own explanation is the useful part and should survive")
}

func TestXOAuth2_ChallengeThatIsNotBase64(t *testing.T) {
	_, err := NewXOAuth2("user@example.com", "token").Next([]byte(`{"status":"401"}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"status":"401"`)
}

func TestXOAuth2_EmptyChallenge(t *testing.T) {
	_, err := NewXOAuth2("user@example.com", "token").Next(nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rejected the access token")
}
