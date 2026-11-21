package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds runtime settings loaded from env and optional YAML.
type Config struct {
	DiscordToken   string            `yaml:"discord_token"`
	GuildAllowlist []string          `yaml:"guild_allowlist"`
	OpenAIBaseURL  string            `yaml:"openai_base_url"`
	OpenAIAPIKey   string            `yaml:"openai_api_key"`
	DefaultModel   string            `yaml:"default_model"`
	SystemPrompt   string            `yaml:"system_prompt"`
	MaxHistory     int               `yaml:"max_history"`
	SQLitePath     string            `yaml:"sqlite_path"`
	// ChannelModels maps Discord channel ID or name → model id.
	ChannelModels map[string]string `yaml:"channel_models"`
	// StreamReplies enables Discord message edits while streaming when possible.
	StreamReplies bool `yaml:"stream_replies"`
}

// Load reads optional YAML from path (if non-empty and present), then overlays
// environment variables. Env always wins for secrets and primary settings.
func Load(path string) (*Config, error) {
	cfg := &Config{
		OpenAIBaseURL: "https://api.openai.com/v1",
		DefaultModel:  "gpt-4o-mini",
		SystemPrompt:  "You are a helpful assistant in Discord. Be concise unless asked for detail.",
		MaxHistory:    40,
		SQLitePath:    "data/grokbot.db",
		ChannelModels: map[string]string{},
		StreamReplies: true,
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("read config: %w", err)
			}
		} else {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parse config: %w", err)
			}
			if cfg.ChannelModels == nil {
				cfg.ChannelModels = map[string]string{}
			}
		}
	}

	overlayEnv(cfg)

	if cfg.DiscordToken == "" {
		return nil, fmt.Errorf("DISCORD_TOKEN is required")
	}
	if cfg.OpenAIAPIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}
	if cfg.MaxHistory < 2 {
		cfg.MaxHistory = 2
	}
	cfg.OpenAIBaseURL = strings.TrimRight(cfg.OpenAIBaseURL, "/")

	return cfg, nil
}

func overlayEnv(cfg *Config) {
	if v := os.Getenv("DISCORD_TOKEN"); v != "" {
		cfg.DiscordToken = v
	}
	if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
		cfg.OpenAIBaseURL = v
	}
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.OpenAIAPIKey = v
	}
	if v := os.Getenv("DEFAULT_MODEL"); v != "" {
		cfg.DefaultModel = v
	}
	if v := os.Getenv("SYSTEM_PROMPT"); v != "" {
		cfg.SystemPrompt = v
	}
	if v := os.Getenv("SQLITE_PATH"); v != "" {
		cfg.SQLitePath = v
	}
	if v := os.Getenv("CONFIG_PATH"); v != "" {
		// consumed by main; ignore here
		_ = v
	}
	if v := os.Getenv("MAX_HISTORY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxHistory = n
		}
	}
	if v := os.Getenv("STREAM_REPLIES"); v != "" {
		cfg.StreamReplies = parseBool(v)
	}
	if v := os.Getenv("GUILD_ALLOWLIST"); v != "" {
		cfg.GuildAllowlist = splitCSV(v)
	}
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// GuildAllowed reports whether the guild may use the bot.
// An empty allowlist means all guilds are allowed.
func (c *Config) GuildAllowed(guildID string) bool {
	if len(c.GuildAllowlist) == 0 {
		return true
	}
	for _, id := range c.GuildAllowlist {
		if id == guildID {
			return true
		}
	}
	return false
}

// ResolveModel picks the model for a channel.
//
// Priority:
//  1. channel_models[channelID]
//  2. channel_models[channelName]
//  3. channel topic: "model: <id>" (first matching line / substring)
//  4. channel name if it looks like a model id (contains "/" or starts with known prefixes)
//  5. default_model
func (c *Config) ResolveModel(channelID, channelName, topic string) string {
	if m, ok := c.ChannelModels[channelID]; ok && m != "" {
		return m
	}
	if m, ok := c.ChannelModels[channelName]; ok && m != "" {
		return m
	}
	if m := ModelFromTopic(topic); m != "" {
		return m
	}
	if looksLikeModelID(channelName) {
		return channelName
	}
	return c.DefaultModel
}

// ModelFromTopic extracts a model id from a channel topic.
// Accepted forms (case-insensitive key):
//
//	model: gpt-4o
//	model=llama3.2
//	[model: groq/llama-3.3-70b-versatile]
func ModelFromTopic(topic string) string {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return ""
	}
	for _, line := range strings.Split(topic, "\n") {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, "[]")
		lower := strings.ToLower(line)
		var rest string
		switch {
		case strings.HasPrefix(lower, "model:"):
			rest = strings.TrimSpace(line[len("model:"):])
		case strings.HasPrefix(lower, "model="):
			rest = strings.TrimSpace(line[len("model="):])
		default:
			continue
		}
		rest = strings.Trim(rest, " `\"'")
		if rest != "" {
			return rest
		}
	}
	return ""
}

func looksLikeModelID(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false
	}
	if strings.Contains(n, "/") {
		return true
	}
	prefixes := []string{"gpt-", "o1", "o3", "o4", "claude-", "llama", "mistral", "gemma", "qwen", "deepseek", "grok-"}
	for _, p := range prefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}
