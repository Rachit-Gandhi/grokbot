package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/discordgo"

	"github.com/Rachit-Gandhi/grokbot/internal/config"
	"github.com/Rachit-Gandhi/grokbot/internal/openai"
	"github.com/Rachit-Gandhi/grokbot/internal/store"
)

const discordMaxContent = 1900

// Bot wires Discord events to the OpenAI-compatible client and session store.
type Bot struct {
	cfg    *config.Config
	sess   *discordgo.Session
	llm    *openai.Client
	store  *store.Store
	log    *slog.Logger
	busy   sync.Map // threadID → struct{} while generating
}

// New creates a bot. Call Start then Open the Discord session separately via Session.
func New(cfg *config.Config, llm *openai.Client, st *store.Store, log *slog.Logger) (*Bot, error) {
	if log == nil {
		log = slog.Default()
	}
	s, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, err
	}
	s.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent |
		discordgo.IntentsGuildMessageReactions

	b := &Bot{cfg: cfg, sess: s, llm: llm, store: st, log: log}
	s.AddHandler(b.onReady)
	s.AddHandler(b.onMessageCreate)
	return b, nil
}

// Session returns the underlying discordgo session.
func (b *Bot) Session() *discordgo.Session { return b.sess }

// Open connects to the Discord gateway.
func (b *Bot) Open() error { return b.sess.Open() }

// Close disconnects.
func (b *Bot) Close() error { return b.sess.Close() }

func (b *Bot) onReady(_ *discordgo.Session, r *discordgo.Ready) {
	b.log.Info("connected", "user", r.User.Username+"#"+r.User.Discriminator)
}

func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot || m.Author.ID == s.State.User.ID {
		return
	}
	if m.GuildID == "" || !b.cfg.GuildAllowed(m.GuildID) {
		return
	}
	content := strings.TrimSpace(m.Content)
	if content == "" {
		return
	}

	// Only handle messages inside threads (forum posts or channel threads).
	ch, err := s.State.Channel(m.ChannelID)
	if err != nil {
		ch, err = s.Channel(m.ChannelID)
		if err != nil {
			b.log.Warn("lookup channel", "err", err)
			return
		}
	}
	if !isThread(ch) {
		// In a parent text channel: create a thread from this message if mentioned or "!" prefix.
		if shouldStartThread(s, m, content) {
			b.startThreadFromMessage(s, m, content)
		}
		return
	}

	parentID := ch.ParentID
	parent, err := s.State.Channel(parentID)
	if err != nil {
		parent, err = s.Channel(parentID)
		if err != nil {
			b.log.Warn("lookup parent channel", "err", err)
			return
		}
	}

	model := b.cfg.ResolveModel(parent.ID, parent.Name, parent.Topic)
	threadID := m.ChannelID

	if _, loaded := b.busy.LoadOrStore(threadID, struct{}{}); loaded {
		_, _ = s.ChannelMessageSendReply(threadID, "⏳ Still working on the previous reply…", m.Reference())
		return
	}
	defer b.busy.Delete(threadID)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	if strings.EqualFold(content, "!reset") || strings.EqualFold(content, "/reset") {
		if err := b.store.Clear(ctx, threadID); err != nil {
			b.replyError(s, m, err)
			return
		}
		_, _ = s.ChannelMessageSendReply(threadID, "Session cleared.", m.Reference())
		return
	}
	if strings.EqualFold(content, "!model") || strings.EqualFold(content, "/model") {
		_, _ = s.ChannelMessageSendReply(threadID, fmt.Sprintf("This channel uses model: `%s`", model), m.Reference())
		return
	}

	userText := stripBotMention(s, content)
	if userText == "" {
		return
	}

	if err := b.store.Append(ctx, threadID, "user", userText); err != nil {
		b.replyError(s, m, err)
		return
	}

	history, err := b.store.History(ctx, threadID, b.cfg.MaxHistory)
	if err != nil {
		b.replyError(s, m, err)
		return
	}

	messages := make([]openai.Message, 0, len(history)+1)
	if b.cfg.SystemPrompt != "" {
		messages = append(messages, openai.Message{Role: "system", Content: b.cfg.SystemPrompt})
	}
	messages = append(messages, history...)

	_ = s.ChannelTyping(threadID)

	var reply string
	if b.cfg.StreamReplies {
		reply, err = b.streamReply(ctx, s, threadID, m, model, messages)
	} else {
		reply, err = b.llm.Complete(ctx, model, messages)
		if err == nil {
			_, err = s.ChannelMessageSendReply(threadID, clip(reply, discordMaxContent), m.Reference())
		}
	}
	if err != nil {
		b.log.Error("completion failed", "model", model, "err", err)
		b.replyError(s, m, err)
		return
	}
	if reply == "" {
		return
	}
	if err := b.store.Append(ctx, threadID, "assistant", reply); err != nil {
		b.log.Warn("persist assistant message", "err", err)
	}
}

func (b *Bot) streamReply(ctx context.Context, s *discordgo.Session, threadID string, m *discordgo.MessageCreate, model string, messages []openai.Message) (string, error) {
	placeholder, err := s.ChannelMessageSendReply(threadID, "…", m.Reference())
	if err != nil {
		// Fall back to non-streaming complete + send.
		text, cerr := b.llm.Complete(ctx, model, messages)
		if cerr != nil {
			return "", cerr
		}
		_, sendErr := s.ChannelMessageSendReply(threadID, clip(text, discordMaxContent), m.Reference())
		return text, sendErr
	}

	var (
		mu       sync.Mutex
		assembled strings.Builder
		lastEdit  time.Time
		editErr   error
	)
	flush := func(force bool) {
		mu.Lock()
		defer mu.Unlock()
		if !force && time.Since(lastEdit) < 800*time.Millisecond {
			return
		}
		text := assembled.String()
		if text == "" {
			text = "…"
		}
		_, editErr = s.ChannelMessageEdit(threadID, placeholder.ID, clip(text, discordMaxContent))
		lastEdit = time.Now()
	}

	full, err := b.llm.Stream(ctx, model, messages, func(delta string) error {
		mu.Lock()
		assembled.WriteString(delta)
		mu.Unlock()
		flush(false)
		return nil
	})
	flush(true)
	if err != nil {
		// If streaming failed but we got nothing, try complete.
		if full == "" {
			text, cerr := b.llm.Complete(ctx, model, messages)
			if cerr != nil {
				_, _ = s.ChannelMessageEdit(threadID, placeholder.ID, "⚠️ "+clip(err.Error(), 500))
				return "", err
			}
			_, _ = s.ChannelMessageEdit(threadID, placeholder.ID, clip(text, discordMaxContent))
			return text, nil
		}
		b.log.Warn("stream ended with error", "err", err)
	}
	if editErr != nil {
		b.log.Warn("discord edit during stream", "err", editErr)
	}
	return full, nil
}

func (b *Bot) startThreadFromMessage(s *discordgo.Session, m *discordgo.MessageCreate, content string) {
	name := threadName(stripBotMention(s, content))
	thread, err := s.MessageThreadStartComplex(m.ChannelID, m.ID, &discordgo.ThreadStart{
		Name:                name,
		AutoArchiveDuration: 10080,
	})
	if err != nil {
		b.log.Warn("create thread", "err", err)
		_, _ = s.ChannelMessageSendReply(m.ChannelID, "Could not start a thread. Ensure the bot can Manage Threads.", m.Reference())
		return
	}
	// Re-dispatch as if the user posted inside the new thread.
	clone := *m
	clone.ChannelID = thread.ID
	clone.Content = stripBotMention(s, content)
	b.onMessageCreate(s, &clone)
}

func (b *Bot) replyError(s *discordgo.Session, m *discordgo.MessageCreate, err error) {
	msg := "⚠️ " + clip(err.Error(), 500)
	if _, sendErr := s.ChannelMessageSendReply(m.ChannelID, msg, m.Reference()); sendErr != nil {
		b.log.Warn("send error reply", "err", sendErr)
	}
}

func isThread(ch *discordgo.Channel) bool {
	if ch == nil {
		return false
	}
	switch ch.Type {
	case discordgo.ChannelTypeGuildPublicThread,
		discordgo.ChannelTypeGuildPrivateThread,
		discordgo.ChannelTypeGuildNewsThread:
		return true
	default:
		return false
	}
}

func shouldStartThread(s *discordgo.Session, m *discordgo.MessageCreate, content string) bool {
	if strings.HasPrefix(content, "!") || strings.HasPrefix(content, "/chat") {
		return true
	}
	if s.State.User == nil {
		return false
	}
	mention := "<@" + s.State.User.ID + ">"
	mentionNick := "<@!" + s.State.User.ID + ">"
	return strings.Contains(content, mention) || strings.Contains(content, mentionNick)
}

func stripBotMention(s *discordgo.Session, content string) string {
	if s.State.User != nil {
		id := s.State.User.ID
		content = strings.ReplaceAll(content, "<@"+id+">", "")
		content = strings.ReplaceAll(content, "<@!"+id+">", "")
	}
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "! ")
	content = strings.TrimPrefix(content, "!")
	content = strings.TrimPrefix(content, "/chat ")
	return strings.TrimSpace(content)
}

func threadName(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return "chat"
	}
	// Discord thread names: 1–100 chars.
	runes := []rune(content)
	if len(runes) > 80 {
		runes = runes[:80]
	}
	return string(runes)
}

func clip(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if len(runes) > max {
		return string(runes[:max-1]) + "…"
	}
	return s
}
