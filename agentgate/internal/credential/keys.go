package credential

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// LoadRSAPrivateKeyPEM reads a PKCS#1 or PKCS#8 PEM RSA private key and
// rejects keys smaller than 2048 bits.
func LoadRSAPrivateKeyPEM(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("credential: read key file: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("credential: no PEM block in key file")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("credential: parse PKCS#1 key: %w", err)
		}
	case "PRIVATE KEY":
		parsed, perr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if perr != nil {
			return nil, fmt.Errorf("credential: parse PKCS#8 key: %w", perr)
		}
		var ok bool
		if key, ok = parsed.(*rsa.PrivateKey); !ok {
			return nil, errors.New("credential: PKCS#8 key is not RSA")
		}
	default:
		return nil, fmt.Errorf("credential: unsupported PEM type %q", block.Type)
	}
	if key.N.BitLen() < minKeyBits {
		return nil, fmt.Errorf("credential: RSA key must be at least %d bits", minKeyBits)
	}
	return key, nil
}

// GenerateEphemeralKey returns a fresh 2048-bit key. Development only: the
// key is lost on restart, so previously issued tokens become unverifiable.
func GenerateEphemeralKey() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, minKeyBits)
}
