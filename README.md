<p align="center">
  <img src="./assets/readme/hero.png" width="100%" alt="Kuromatsu — the black-pine bonsai logo beside the promise: a personal AI agent that runs a 1-bit LLM inside its own binary, on a 1 GB VM">
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#configuration">Configuration</a> ·
  <a href="#resumo-em-português">Português</a>
</p>

**Kuromatsu** is a personal AI agent written in Go that lives 24/7 on very small machines. It
ships a 1-bit language model *inside its own binary* (no model server, no API key), talks to you
over Telegram, WhatsApp or its own channel, runs scheduled routines, and keeps a long-term memory —
reaching for a larger external model only when the language actually needs it.

Like the bonsai it is named after (黒松, *black pine*), it is grown by pruning: every token, every
megabyte and every CPU second on a burst-credit VM is treated as a budget.

## Running in production

The reference deployment is a Google Cloud **e2-micro** (shared vCPU, 1 GB RAM) running the
native binary under systemd — no Docker. Numbers measured on that VM:

| | |
| --- | --- |
| Memory ceiling for the whole service | `MemoryMax=900M` (systemd), model loaded |
| Native model | Bonsai-1.7B `Q1_0` GGUF, ~237 MB, in-process via llama.cpp |
| KV prefix-cache reuse on repeated turns | **93%** of prompt tokens served from cache |
| Native generation on the throttled vCPU | ~0.5 tokens/s — slow by design, so heavy language goes external |
| First reply through an external model | 1.9 s (Ollama-compatible endpoint) |
| An idle heartbeat with nothing to do | 0 tokens, 0 model calls |


## How it works

<p align="center">
  <img src="./assets/readme/how-it-works.svg" width="100%" alt="Every message passes a zero-token reflex, a focus window that loads only what the turn needs, the in-process Bonsai model with KV prefix caching, and an external model only when it pays off. In the background run quiet routines, runstate and memguard, nightly sleep consolidation and per-turn telemetry.">
</p>

- **Reflexes** answer pattern-matched messages instantly, without calling any model.
- **Focus windows** route each turn (by origin and content) to a small set of tools, skills and
  memory, so the prompt stays short and cacheable.
- **Native inference** runs Bonsai-1.7B through llama.cpp linked with cgo; the KV cache keeps the
  stable prompt prefix between turns instead of re-reading it.
- **External models** (any OpenAI- or Ollama-compatible API) take the turns that need more
  language; credentials found in outgoing messages are masked and restored locally.
- **Routines** are cron jobs that run plain commands and stay silent when everything is fine; the
  model is woken only to interpret an anomaly, within a daily cap.
- **Runstate + memguard** keep a bitmask of what the agent is doing and protect the 1 GB budget,
  reading the service's real cgroup limit and kernel memory pressure (PSI).
- **Sleep** consolidates long-term memory at night using an external model — never the 1-bit one.
- **Telemetry** records tokens, cache hits and latency per turn in SQLite; `/stats` summarizes it.

## Quick start

### Install a release (no Go, no root)

```bash
# Linux, macOS, Termux
curl -fsSL https://raw.githubusercontent.com/andre25costa-code/kuromatsu/main/scripts/install.sh | sh
```

```powershell
# Windows (PowerShell 5.1+)
irm https://raw.githubusercontent.com/andre25costa-code/kuromatsu/main/scripts/install.ps1 | iex
```

The installer downloads the archive for your OS and CPU from the latest release, checks its
SHA-256 against `checksums.txt`, installs the binary in `~/.local/bin` (Termux: `$PREFIX/bin`;
Windows: `%LOCALAPPDATA%\Kuromatsu\bin`), adds that directory to your user PATH and runs
`kuromatsu onboard` (`--non-interactive` when there is no terminal). It never uses sudo. On
Linux/macOS the PATH goes into `~/.profile`, plus `~/.bash_profile`, `~/.bash_login`,
`~/.bashrc` and `~/.zshrc` when present (fish: run `fish_add_path ~/.local/bin`).

| Variable | Default | Use |
|---|---|---|
| `KUROMATSU_INSTALL_BASE_URL` | latest GitHub release | where the archives are; `file://` to test a local build |
| `KUROMATSU_INSTALL_DIR` | see above | install directory |
| `KUROMATSU_INSTALL_NO_ONBOARD` | empty | `1` skips `kuromatsu onboard` |

macOS binaries are not notarized: a browser download is blocked by Gatekeeper, the `curl`
install is not.

To remove it: `kuromatsu uninstall` deletes the binary and the PATH lines and keeps your data;
`kuromatsu uninstall --purge` also deletes the data directory after writing
`~/kuromatsu-backup-<date>.zip`. It refuses directories without a Kuromatsu `config.json` or
`.security.yml`, and your home directory or any directory containing it; it leaves alone a
binary installed by a package manager (`apt remove kuromatsu`) and, on Windows, a PATH entry
the installer did not add.

### Build from source

**Requirements:** Go 1.25+ and git. The pure-Go build needs nothing else and runs on Linux,
macOS, Windows and Android/Termux (`pkg install golang git`), on amd64, arm64 and 32-bit ARM.

```bash
git clone https://github.com/andre25costa-code/kuromatsu.git
cd kuromatsu
go build -o kuromatsu ./cmd/kuromatsu   # on Windows: -o kuromatsu.exe

./kuromatsu onboard                     # setup wizard: creates ~/.kuromatsu with config and workspace
./kuromatsu agent -m "What can you do?" # talk to it from the terminal
./kuromatsu gateway                     # starts the agent and its channels
```

**First run.** In a terminal, `kuromatsu onboard` asks six questions, each with a default in
brackets (Enter accepts it; `q` quits without writing anything, and onboard then exits with
code 2, which the installers report as "setup skipped" without failing the install):

1. language (Português or English; written as a line in the workspace `USER.md`, which sets
   the language the agent answers in);
2. model provider (OpenAI-compatible, local Ollama, Anthropic, Gemini or OpenRouter), with a
   connection test that lists the models to pick from; if the test fails you can retry, edit,
   save anyway with a model id, or skip;
3. channel: Telegram (bot token and **your** numeric Telegram ID, which becomes `allow_from`)
   or none;
4. hardware profile (see below);
5. timezone (the `timezone` key; the default is `TZ` or the zone read from `/etc/localtime` or
   `/etc/timezone`, and when none is known, Enter leaves it empty, meaning the system zone);
6. permission preset:

| Key | Safe | Assistant | Advanced |
|---|---|---|---|
| `agents.defaults.restrict_to_workspace` | true | true | true |
| `tools.exec.enabled` | false | false | true |
| `tools.exec.allow_remote` | false | false | false |
| `tools.web.enabled` | false | true | true |
| `tools.cron.enabled` | false | true | true |
| `tools.cron.allow_command` | false | false | true |

No preset lets exec run from Telegram (`allow_remote` stays off: exec only runs from the
terminal and internal channels); turning that on is a deliberate manual edit. On a new config,
Enter at step 6 picks Assistant (exec off). `onboard --non-interactive` (also used when there
is no terminal, e.g. installers and Docker) asks nothing and keeps the built-in defaults,
which equal Advanced (exec on, internal channels only). Running `onboard` again starts from the
current config: every step offers "Keep current" as its default, so Enter through all of them
changes nothing in the config except writing a detected zone into an empty `timezone`.

The `timezone` key (an IANA name such as `America/Sao_Paulo`; env `KUROMATSU_TIMEZONE`) sets
the local time of the process. The `TZ` variable, when set, wins over it, and a change takes
effect after a restart. Empty keeps `TZ` or the system zone.

To add or switch a model later: `./kuromatsu model add -b https://openrouter.ai/api/v1 -k <your-key>`
(any OpenAI-compatible endpoint: lists its models, saves the one you pick and makes it the
default). For a local Ollama: `./kuromatsu model add -b http://localhost:11434/v1 -k ollama`.
`make build` produces the same binary with release flags under `build/`.

With the in-process model (Linux x86-64 with AVX2; needs `cmake` and a C/C++ toolchain):

```bash
git submodule update --init llama.cpp
make build-native-x86-64   # links llama.cpp (AVX2) -> build/kuromatsu-native-linux-amd64
scripts/download-model.sh  # fetches Bonsai-1.7B-Q1_0.gguf and checks its SHA-256
```

### Release archives

Each GitHub release carries prebuilt binaries under fixed, version-less names, each with the
`kuromatsu` binary (or `kuromatsu.exe`) and `LICENSE`:

| Platform | Archive |
|---|---|
| Linux x86-64 / arm64 | `kuromatsu_Linux_x86_64.tar.gz`, `kuromatsu_Linux_arm64.tar.gz` |
| Linux 32-bit ARM | `kuromatsu_Linux_armv6.tar.gz`, `kuromatsu_Linux_armv7.tar.gz` |
| Linux, other | `kuromatsu_Linux_riscv64.tar.gz`, `kuromatsu_Linux_loong64.tar.gz`, `kuromatsu_Linux_s390x.tar.gz` |
| macOS | `kuromatsu_Darwin_arm64.tar.gz`, `kuromatsu_Darwin_x86_64.tar.gz` |
| Windows | `kuromatsu_Windows_x86_64.zip`, `kuromatsu_Windows_arm64.zip` |
| Android / Termux (arm64) | `kuromatsu_Android_arm64.tar.gz` |
| FreeBSD / NetBSD | `kuromatsu_Freebsd_x86_64.tar.gz`, `kuromatsu_Freebsd_arm64.tar.gz`, `kuromatsu_Freebsd_armv6.tar.gz`, `kuromatsu_Freebsd_armv7.tar.gz`, `kuromatsu_Netbsd_x86_64.tar.gz` |
| Linux x86-64, native model (AVX2) | `kuromatsu-native_Linux_x86_64.tar.gz` |

`checksums.txt` holds the SHA-256 of every archive. On Termux, use the Android archive rather
than the Linux arm64 one: it finds the system CA certificates and avoids system calls that
Android's seccomp filter kills.

`kuromatsu update` downloads the archive whose name matches the running binary exactly and checks
its SHA-256 before replacing itself. A native build only updates from
`kuromatsu-native_Linux_x86_64.tar.gz`, and refuses a release that lacks it rather than
dropping local inference.

## Configuration

Configuration lives in `~/.kuromatsu/config.json`; secrets go in `.security.yml`. A minimal
setup with an external model first and the native model as fallback:

```json
{
  "agents": {
    "defaults": {
      "model_name": "cloud",
      "model_fallbacks": ["bonsai-local"],
      "focus": { "enabled": true }
    }
  },
  "model_list": [
    { "model_name": "cloud", "provider": "ollama", "model": "<model>",
      "api_base": "https://<your-endpoint>/v1" },
    { "model_name": "bonsai-local", "provider": "native", "model": "Bonsai-1.7B-Q1_0",
      "tool_schema_transform": "compact" }
  ]
}
```

**Hardware profiles.** `kuromatsu onboard` on a new config detects RAM (the smaller of total
RAM and the cgroup limit) and CPUs and writes one of four profiles as plain config values:
Ultra-Light (< 768 MiB), Light (< 2 GiB), Balanced (< 8 GiB, or fewer than 4 CPUs) or
Performance. A profile sets the native model's context size and threads, the memguard limits,
the sub-turn limits and the heartbeat interval; Light equals the built-in defaults. An existing
config only changes with `kuromatsu onboard --profile <ultra-light|light|balanced|performance|auto>`
or an explicit choice in the wizard,
and `kuromatsu status` shows the detected hardware and which profile the config matches.

Channels: **Telegram**, **WhatsApp** and the built-in **Pico** channel. Every channel is
fail-closed: a channel with an empty `allow_from` **rejects everyone** and logs an error at
boot (the first dropped message also logs the sender ID, so you can copy it). List your own
ID, or use `"*"` to open the bot to anyone on purpose:

```json
"channel_list": {
  "telegram": { "enabled": true, "type": "telegram", "allow_from": ["<your-telegram-user-id>"] }
}
```

Upgrading from a version where an empty `allow_from` accepted anyone? Set `allow_from` on
every enabled channel before restarting, or the bot goes silent.

**Pico.** Its sender is fixed and access is gated by the token (compared in constant time), so
use `"allow_from": ["*"]`. The built-in default `pico` entry has no `allow_from`: if you only
write `"enabled": true`, add `"allow_from": ["*"]` as well. `allow_origins` (under `settings`)
is the list of Origins accepted on the WebSocket; empty accepts any Origin, and the token is
still required.

Shell commands requested from a chat are off by default; enable them only for channels you
trust with `"tools": { "exec": { "allow_remote": true } }`.

Agent-loop guards (all under `agents.defaults`): `user_turn_max_minutes` caps a whole user
turn, **30 minutes by default** — this also applies to the CLI and to turns on the slow native
model, so a very long legitimate turn ends with an error naming the key; set it to `0` to turn
the cap off. `max_repeated_tool_calls` (default 3, `0` = off) ends a turn that keeps making the
same tool call with the same result, and `subturn.max_concurrent_global` (default 8 per depth
level) caps concurrent sub-turns process-wide. `user_turn_max_minutes` and the sub-turn pools
(`subturn.max_concurrent_global`, `subturn.max_depth`) are read at startup: restart after
changing them, a live config reload does not apply them.

Scheduling:
`kuromatsu cron add --command '…' --quiet` for deterministic routines, `HEARTBEAT.md` for
periodic tasks that need judgment. The workspace (`AGENT.md`, `SOUL.md`, `USER.md`,
`HEARTBEAT.md`, `memory/`) defines who the agent is; the repository ships a neutral template.

**Web UI.** The gateway serves a configuration page at `http://localhost:18790/ui/`
(`kuromatsu ui open` prints the address): assistant files, models, channels and permission
presets, skills, automations, heartbeat and status, in pt-BR, usable on a phone. It answers only
from loopback, refuses requests carrying proxy headers (`X-Forwarded-For`, `Forwarded`, ...)
and any `Host` other than `localhost`, `127.0.0.1` or `[::1]`, and needs the access token
printed by `kuromatsu ui token` (created by `onboard`, stored in `.security.yml`; the gateway
never creates it and does not serve the page without one). From another machine, use an SSH
tunnel (`ssh -L 18790:localhost:18790 <host>`); do not tunnel or proxy the whole gateway port
to the internet. Five wrong logins in a minute lock the login for that minute (on loopback
this applies to every local process; the Bearer token keeps working for scripts). On a shared
machine, set `"webui": {"enabled": false}`. `webui.enabled`, `webui.allow_remote` and the
token apply on the next gateway start.

The full reference — every config key, channel, tool, schedule and the sleep mode — is the
`kuromatsu-docs` skill the agent itself reads: [`workspace/skills/kuromatsu-docs`](./workspace/skills/kuromatsu-docs/SKILL.md).

## Deployment

The production path is a native binary under systemd on Linux x86-64:
[`deploy/systemd/`](./deploy/systemd) holds the unit (with `MemoryMax`) and
[`deploy/`](./deploy) the sysctl and nftables tuning. Copy the binary, install the unit and
start it — no container runtime needed. Docker files remain in [`docker/`](./docker) for local use.

## Limits

- Native generation is slow on shared CPUs; Kuromatsu is built to need the native model rarely,
  not to make it fast.
- The native build targets x86-64 with AVX2 and FMA; other platforms use the pure-Go build with
  external models. On a CPU without AVX2/FMA the native binary refuses the native model (`kuromatsu
  status` shows `Native CPU ✗ missing ...`), but llama.cpp's own startup code may still crash
  there before Kuromatsu runs: on such a CPU, use the pure-Go release.
- Memory search is lexical (SQLite FTS5, used by `memory: retrieved`); there is no semantic
  (embeddings) search yet.

## Resumo em português

O **Kuromatsu** é um agente pessoal de IA em Go que roda 24/7 em máquinas pequenas — a referência
é uma VM e2-micro do Google Cloud com 1 GB de RAM. Ele carrega um modelo de linguagem de 1 bit
(Bonsai-1.7B) **dentro do próprio binário**, conversa por Telegram, WhatsApp ou canal próprio,
executa rotinas agendadas que só acordam o modelo quando algo sai do normal, e consolida a
memória de longo prazo à noite usando um modelo externo. Cada token, megabyte e segundo de CPU é
tratado como orçamento.

## Credits

Kuromatsu started as a fork of [PicoClaw](https://github.com/sipeed/picoclaw) (MIT). Native
inference uses [llama.cpp](https://github.com/ggml-org/llama.cpp) through the
[PrismML-Eng fork](https://github.com/PrismML-Eng/llama.cpp) and the
[Bonsai-1.7B](https://huggingface.co/prism-ml/Bonsai-1.7B-gguf) model by prism-ml.

## License

[MIT](./LICENSE).
