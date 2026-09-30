package config

import "testing"

func TestLoadToolBootstrapFile(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolBootstrapFile != "" {
		t.Errorf("default = %q", cfg.ToolBootstrapFile)
	}
	t.Setenv(envPrefix+"TOOL_BOOTSTRAP_FILE", "/etc/agentgate/tools.bootstrap.json")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolBootstrapFile != "/etc/agentgate/tools.bootstrap.json" {
		t.Errorf("got %q", cfg.ToolBootstrapFile)
	}
}
