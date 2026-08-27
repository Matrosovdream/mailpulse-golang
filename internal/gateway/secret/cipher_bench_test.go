package secret

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// Every mailbox resolve decrypts a credential blob, so this cost is paid once
// per account per poll — not once per request. At ten thousand accounts on a
// two minute interval that is roughly eighty decrypts a second, sustained,
// before any mail is fetched.

func benchCipher(b *testing.B) *Cipher {
	b.Helper()

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		b.Fatal(err)
	}

	cipher, err := NewCipher(base64.StdEncoding.EncodeToString(key))
	if err != nil {
		b.Fatal(err)
	}

	return cipher
}

// blob sizes bracket what mail_accounts.credentials actually holds: a username
// and password is small, and an OAuth blob carrying a refresh token is not.
var blobSizes = []int{64, 512, 4096}

func BenchmarkCipherEncrypt(b *testing.B) {
	cipher := benchCipher(b)

	for _, size := range blobSizes {
		plaintext := strings.Repeat("c", size)

		b.Run(fmt.Sprintf("size=%dB", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))

			for i := 0; i < b.N; i++ {
				if _, err := cipher.Encrypt(plaintext); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCipherDecrypt(b *testing.B) {
	cipher := benchCipher(b)

	for _, size := range blobSizes {
		encrypted, err := cipher.Encrypt(strings.Repeat("c", size))
		if err != nil {
			b.Fatal(err)
		}

		b.Run(fmt.Sprintf("size=%dB", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))

			for i := 0; i < b.N; i++ {
				if _, err := cipher.Decrypt(encrypted); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCipherNew measures key setup on its own. If it is not negligible
// then constructing a cipher per operation, rather than once at bootstrap,
// would be a real cost — worth knowing before anyone is tempted to.
func BenchmarkCipherNew(b *testing.B) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		b.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(key)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := NewCipher(encoded); err != nil {
			b.Fatal(err)
		}
	}
}
