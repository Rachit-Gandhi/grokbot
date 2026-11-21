package main

import (
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Rachit-Gandhi/grokbot/internal/bot"
	"github.com/Rachit-Gandhi/grokbot/internal/config"
	"github.com/Rachit-Gandhi/grokbot/internal/openai"
	"github.com/Rachit-Gandhi/grokbot/internal/store"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "configs/config.yaml"
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(1)
	}

	st, err := store.Open(cfg.SQLitePath)
	if err != nil {
		log.Error("sqlite", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	llm := openai.New(cfg.OpenAIBaseURL, cfg.OpenAIAPIKey)
	b, err := bot.New(cfg, llm, st, log)
	if err != nil {
		log.Error("bot init", "err", err)
		os.Exit(1)
	}

	if err := b.Open(); err != nil {
		log.Error("discord open", "err", err)
		os.Exit(1)
	}
	defer b.Close()

	log.Info("grokbot running",
		"base_url", cfg.OpenAIBaseURL,
		"default_model", cfg.DefaultModel,
		"sqlite", cfg.SQLitePath,
	)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")
}
