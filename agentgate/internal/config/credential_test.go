package config

import (
	"testing"
	"time"
)

func TestLoadCredentialDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CredentialIssuer != "agentgate" || cfg.CredentialKeyID != "agentgate-cred-1" ||
		cfg.CredentialLifetime != 60*time.Second || !cfg.RequireDownstreamCredential ||
		cfg.CredentialSigningKeyFile != "" || len(cfg.CredentialAudiences) != 0 {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadCredentialAudiences(t *testing.T) {
	t.Setenv(envPrefix+"CREDENTIAL_AUDIENCES", "mcp-probe=aud-a, default=aud-b")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CredentialAudiences) != 2 || cfg.CredentialAudiences["mcp-probe"] != "aud-a" || cfg.CredentialAudiences["default"] != "aud-b" {
		t.Errorf("audiences = %v", cfg.CredentialAudiences)
	}
}

func TestLoadCredentialAudiencesMalformed(t *testing.T) {
	for _, v := range []string{"a=", "=b", "ab", "a=b,,c=d", "a=b,c"} {
		t.Setenv(envPrefix+"CREDENTIAL_AUDIENCES", v)
		if _, err := Load(); err == nil {
			t.Errorf("Load() with %q: want error", v)
		}
	}
}

func TestLoadRequireDownstreamCredentialInvalid(t *testing.T) {
	t.Setenv(envPrefix+"REQUIRE_DOWNSTREAM_CREDENTIAL", "maybe")
	if _, err := Load(); err == nil {
		t.Fatal("want error")
	}
	t.Setenv(envPrefix+"REQUIRE_DOWNSTREAM_CREDENTIAL", "false")
	cfg, err := Load()
	if err != nil || cfg.RequireDownstreamCredential {
		t.Fatalf("cfg=%v err=%v", cfg.RequireDownstreamCredential, err)
	}
}

func TestLoadNormalizesEnvironment(t *testing.T) {
	t.Setenv(envPrefix+"OIDC_ISSUER_URL", "https://idp.example")
	t.Setenv(envPrefix+"OIDC_AUDIENCE", "agentgate")
	t.Setenv(envPrefix+"DATABASE_URL", "postgres://localhost:5432/test?sslmode=require")
	t.Setenv(envPrefix+"AUDIT_SALT", "test-audit-salt-production")
	t.Setenv(envPrefix+"CREDENTIAL_SIGNING_KEY_FILE", "/path/to/key.pem")
	t.Setenv(envPrefix+"CREDENTIAL_AUDIENCES", "mcp-probe=probe-aud")
	for in, want := range map[string]string{
		"Production":   "production",
		" PRODUCTION ": "production",
		"prod":         "production",
		"Staging":      "staging",
		"":             defaultEnvironment,
		"   ":          defaultEnvironment,
	} {
		t.Setenv(envPrefix+"ENV", in)
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Environment != want {
			t.Errorf("ENV %q => %q, want %q", in, cfg.Environment, want)
		}
	}
}
