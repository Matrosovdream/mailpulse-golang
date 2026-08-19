package secret

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testKey(t *testing.T) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
}

func TestNewCipher_RejectsBadKeys(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"not base64":   "!!!not-base64!!!",
		"wrong length": base64.StdEncoding.EncodeToString([]byte("too-short")),
	}

	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			cipher, err := NewCipher(key)
			assert.Nil(t, cipher)
			assert.Error(t, err, "a bad key must stop the app rather than silently degrade")
		})
	}
}

func TestCipher_RoundTrip(t *testing.T) {
	cipher, err := NewCipher(testKey(t))
	require.NoError(t, err)

	secrets := []string{
		`{"password":"app-specific-password"}`,
		"unicode: пароль 密码",
		strings.Repeat("long", 5000),
	}

	for _, plaintext := range secrets {
		encrypted, err := cipher.Encrypt(plaintext)
		require.NoError(t, err)
		assert.NotContains(t, encrypted, plaintext, "ciphertext must not leak the plaintext")

		decrypted, err := cipher.Decrypt(encrypted)
		require.NoError(t, err)
		assert.Equal(t, plaintext, decrypted)
	}
}

// AES-GCM uses a fresh nonce per call, so the same input must not produce the
// same ciphertext — otherwise equal passwords would be visible as equal rows.
func TestCipher_EncryptIsNotDeterministic(t *testing.T) {
	cipher, err := NewCipher(testKey(t))
	require.NoError(t, err)

	first, err := cipher.Encrypt("same input")
	require.NoError(t, err)
	second, err := cipher.Encrypt("same input")
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

func TestCipher_EmptyStringStaysEmpty(t *testing.T) {
	cipher, err := NewCipher(testKey(t))
	require.NoError(t, err)

	encrypted, err := cipher.Encrypt("")
	require.NoError(t, err)
	assert.Equal(t, "", encrypted, "an absent secret should not become ciphertext")

	decrypted, err := cipher.Decrypt("")
	require.NoError(t, err)
	assert.Equal(t, "", decrypted)
}

func TestCipher_DecryptRejectsTamperingAndWrongKey(t *testing.T) {
	cipher, err := NewCipher(testKey(t))
	require.NoError(t, err)

	encrypted, err := cipher.Encrypt("mailbox password")
	require.NoError(t, err)

	t.Run("wrong key", func(t *testing.T) {
		other, err := NewCipher(base64.StdEncoding.EncodeToString([]byte("ffffffffffffffffffffffffffffffff")))
		require.NoError(t, err)

		_, err = other.Decrypt(encrypted)
		assert.Error(t, err, "a rotated or wrong key must fail loudly, not return garbage")
	})

	t.Run("tampered ciphertext", func(t *testing.T) {
		raw, err := base64.StdEncoding.DecodeString(encrypted)
		require.NoError(t, err)
		raw[len(raw)-1] ^= 0xFF

		_, err = cipher.Decrypt(base64.StdEncoding.EncodeToString(raw))
		assert.Error(t, err, "GCM must reject a modified payload")
	})

	t.Run("not base64", func(t *testing.T) {
		_, err := cipher.Decrypt("!!!")
		assert.Error(t, err)
	})

	t.Run("shorter than the nonce", func(t *testing.T) {
		_, err := cipher.Decrypt(base64.StdEncoding.EncodeToString([]byte("tiny")))
		assert.Error(t, err)
	})
}
