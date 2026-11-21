# GrokBot

Self-hosted **Discord → OpenAI-compatible LLM** bot written in Go.

- **One Discord guild** (or more via allowlist): each **channel** maps to one **model**
- Each **thread** is a **chat session** (history stored in SQLite)
- Works with any OpenAI-compatible Chat Completions API (OpenAI, Groq, OpenRouter, local Ollama, vLLM, LM Studio, …)

## How it works

| Discord concept | GrokBot concept |
|-----------------|-----------------|
| Text / forum channel | Model (via topic, config, or channel name) |
| Thread (or forum post) | Conversation session |
| Message in a thread | User turn → API → bot reply in the same thread |

### Channel → model mapping

Resolved in this order:

1. `channel_models` in YAML keyed by **channel ID**
2. `channel_models` keyed by **channel name**
3. Channel **topic** containing `model: <id>` (or `model=<id>`)
4. Channel **name** if it looks like a model id (`gpt-…`, `llama…`, `provider/model`, …)
5. `DEFAULT_MODEL` / `default_model`

**Recommended:** set the channel topic to:

```text
model: llama3.2
```

or

```text
model: groq/llama-3.3-70b-versatile
```

Topic changes apply on the next message (no restart).

### Starting a chat

- **Create a thread** in the channel and chat there, or
- **@mention the bot** (or prefix with `!`) in the parent channel — the bot starts a thread from that message and replies inside it.

### Session commands (inside a thread)

| Command | Effect |
|---------|--------|
| `!reset` | Clear this thread’s history |
| `!model` | Show the model used for this channel |

## Discord bot setup

1. Open [Discord Developer Portal](https://discord.com/developers/applications) → **New Application**.
2. **Bot** → Add Bot → copy the **token** → `DISCORD_TOKEN`.
3. Enable **Privileged Gateway Intent**: **Message Content Intent**.
4. **OAuth2 → URL Generator**:
   - Scopes: `bot`
   - Permissions: `View Channels`, `Send Messages`, `Send Messages in Threads`, `Create Public Threads`, `Manage Threads`, `Read Message History`, `Embed Links` (optional)
5. Invite the bot to your server with the generated URL.
6. Create channels per model (or one channel and set the topic / config map).
7. Optionally set `GUILD_ALLOWLIST` to your server’s snowflake ID (Server Settings → Widget, or right-click server with Developer Mode).

## Configuration

Copy `.env.example` → `.env` and fill in secrets:

```bash
cp .env.example .env
# edit .env
```

| Variable | Required | Description |
|----------|----------|-------------|
| `DISCORD_TOKEN` | yes | Discord bot token |
| `OPENAI_API_KEY` | yes | API key (any value for local Ollama if unused) |
| `OPENAI_BASE_URL` | no | Default `https://api.openai.com/v1` |
| `DEFAULT_MODEL` | no | Fallback model id |
| `GUILD_ALLOWLIST` | no | Comma-separated guild IDs |
| `SYSTEM_PROMPT` | no | System message prepended to every turn |
| `MAX_HISTORY` | no | Max stored messages per thread (default 40) |
| `STREAM_REPLIES` | no | Edit-in-place streaming (`true`/`false`) |
| `SQLITE_PATH` | no | DB path (default `data/grokbot.db`) |
| `CONFIG_PATH` | no | Optional YAML (default `configs/config.yaml`) |

Optional YAML: see [`configs/config.example.yaml`](configs/config.example.yaml). Environment variables override YAML for secrets and primary settings.

### Example providers

**OpenAI**

```env
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=sk-...
DEFAULT_MODEL=gpt-4o-mini
```

**Groq**

```env
OPENAI_BASE_URL=https://api.groq.com/openai/v1
OPENAI_API_KEY=gsk_...
DEFAULT_MODEL=llama-3.3-70b-versatile
```

**OpenRouter**

```env
OPENAI_BASE_URL=https://openrouter.ai/api/v1
OPENAI_API_KEY=sk-or-...
DEFAULT_MODEL=openai/gpt-4o-mini
```

**Ollama (local on the VPS)**

```env
OPENAI_BASE_URL=http://127.0.0.1:11434/v1
OPENAI_API_KEY=ollama
DEFAULT_MODEL=llama3.2
```

If GrokBot runs in Docker and Ollama on the host, use `http://172.17.0.1:11434/v1` (Linux) or `host.docker.internal`.

## Deploy on a VPS

### Docker Compose (recommended)

```bash
git clone https://github.com/Rachit-Gandhi/grokbot.git
cd grokbot
cp .env.example .env
# edit .env
mkdir -p data
docker compose up -d --build
docker compose logs -f
```

### systemd binary

```bash
# on the VPS
sudo useradd -r -s /usr/sbin/nologin grokbot
sudo mkdir -p /opt/grokbot/data
cd /tmp && git clone https://github.com/Rachit-Gandhi/grokbot.git && cd grokbot
go build -o grokbot ./cmd/grokbot
sudo cp grokbot /opt/grokbot/
sudo cp .env.example /opt/grokbot/.env   # edit secrets
sudo cp deploy/grokbot.service /etc/systemd/system/
sudo chown -R grokbot:grokbot /opt/grokbot
sudo systemctl daemon-reload
sudo systemctl enable --now grokbot
sudo journalctl -u grokbot -f
```

Requires Go 1.22+ to build.

## Development

```bash
cp .env.example .env
go test ./...
go run ./cmd/grokbot
```

## License

MIT
