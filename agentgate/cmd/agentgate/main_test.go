package main

import (
	"io"
	"log/slog"
	"testing"

	"github.com/Dynamisch-LLC/agentgate/internal/config"
)

func TestLoadSigningKeyProductionWithoutFileFails(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Setenv("AGENTGATE_OIDC_ISSUER_URL", "https://idp.example")
	t.Setenv("AGENTGATE_OIDC_AUDIENCE", "agentgate")
	for _, env := range []string{"production", "Production", " PRODUCTION ", "prod"} {
		t.Setenv("AGENTGATE_ENV", env)
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loadSigningKey(cfg, logger); err == nil {
			t.Errorf("env %q: want error without key file", env)
		}
	}
}

func TestLoadSigningKeyDevelopmentEphemeral(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Setenv("AGENTGATE_ENV", "development")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if key, err := loadSigningKey(cfg, logger); err != nil || key == nil {
		t.Fatalf("key=%v err=%v", key, err)
	}
}
