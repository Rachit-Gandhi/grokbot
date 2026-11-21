package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModelFromTopic(t *testing.T) {
	cases := []struct {
		topic string
		want  string
	}{
		{"model: gpt-4o", "gpt-4o"},
		{"MODEL=llama3.2", "llama3.2"},
		{"[model: groq/llama-3.3-70b-versatile]", "groq/llama-3.3-70b-versatile"},
		{"hello\nmodel: o1-mini\nworld", "o1-mini"},
		{"no model here", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := ModelFromTopic(tc.topic); got != tc.want {
			t.Fatalf("ModelFromTopic(%q)=%q want %q", tc.topic, got, tc.want)
		}
	}
}

func TestResolveModelPriority(t *testing.T) {
	cfg := &Config{
		DefaultModel:  "default-model",
		ChannelModels: map[string]string{"chan-id": "from-id", "named": "from-name"},
	}
	if got := cfg.ResolveModel("chan-id", "named", "model: from-topic"); got != "from-id" {
		t.Fatalf("id override: %s", got)
	}
	if got := cfg.ResolveModel("other", "named", "model: from-topic"); got != "from-name" {
		t.Fatalf("name override: %s", got)
	}
	if got := cfg.ResolveModel("other", "other", "model: from-topic"); got != "from-topic" {
		t.Fatalf("topic: %s", got)
	}
	if got := cfg.ResolveModel("other", "gpt-4o", ""); got != "gpt-4o" {
		t.Fatalf("channel name as model: %s", got)
	}
	if got := cfg.ResolveModel("other", "general", ""); got != "default-model" {
		t.Fatalf("default: %s", got)
	}
}

func TestGuildAllowed(t *testing.T) {
	cfg := &Config{}
	if !cfg.GuildAllowed("any") {
		t.Fatal("empty allowlist should allow all")
	}
	cfg.GuildAllowlist = []string{"g1"}
	if cfg.GuildAllowed("g2") {
		t.Fatal("expected deny")
	}
	if !cfg.GuildAllowed("g1") {
		t.Fatal("expected allow")
	}
}

func TestLoadFromEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.yaml")

	t.Setenv("DISCORD_TOKEN", "tok")
	t.Setenv("OPENAI_API_KEY", "key")
	t.Setenv("OPENAI_BASE_URL", "http://localhost:11434/v1/")
	t.Setenv("DEFAULT_MODEL", "llama3.2")
	t.Setenv("GUILD_ALLOWLIST", "a, b")
	t.Setenv("STREAM_REPLIES", "false")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DiscordToken != "tok" || cfg.OpenAIAPIKey != "key" {
		t.Fatalf("secrets not loaded")
	}
	if cfg.OpenAIBaseURL != "http://localhost:11434/v1" {
		t.Fatalf("base url: %s", cfg.OpenAIBaseURL)
	}
	if cfg.DefaultModel != "llama3.2" {
		t.Fatalf("model: %s", cfg.DefaultModel)
	}
	if cfg.StreamReplies {
		t.Fatal("stream should be false")
	}
	if len(cfg.GuildAllowlist) != 2 {
		t.Fatalf("allowlist: %#v", cfg.GuildAllowlist)
	}
	_ = os.Remove(path)
}
