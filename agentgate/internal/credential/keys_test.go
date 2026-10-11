package credential

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func writePEM(t *testing.T, typ string, der []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadRSAPrivateKeyPEM(t *testing.T) {
	k := testKey(t, 2048)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{
		"pkcs1": writePEM(t, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(k)),
		"pkcs8": writePEM(t, "PRIVATE KEY", pkcs8),
	} {
		got, err := LoadRSAPrivateKeyPEM(path)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !got.Equal(k) {
			t.Errorf("%s: key mismatch", name)
		}
	}
}

func TestLoadRSAPrivateKeyPEM_Rejects(t *testing.T) {
	small := testKey(t, 1024)
	if _, err := LoadRSAPrivateKeyPEM(writePEM(t, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(small))); err == nil {
		t.Error("1024-bit accepted")
	}
	if _, err := LoadRSAPrivateKeyPEM(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Error("missing file accepted")
	}
	bad := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(bad, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRSAPrivateKeyPEM(bad); err == nil {
		t.Error("non-PEM accepted")
	}
	if _, err := LoadRSAPrivateKeyPEM(writePEM(t, "PRIVATE KEY", []byte("garbage"))); err == nil {
		t.Error("garbage DER accepted")
	}
}

func TestGenerateEphemeralKey(t *testing.T) {
	k, err := GenerateEphemeralKey()
	if err != nil {
		t.Fatal(err)
	}
	if k.N.BitLen() < 2048 {
		t.Fatalf("bits: %d", k.N.BitLen())
	}
}
