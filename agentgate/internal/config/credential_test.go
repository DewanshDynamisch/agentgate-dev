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
