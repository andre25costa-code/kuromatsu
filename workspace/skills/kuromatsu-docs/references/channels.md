# Canais (chat apps)

Fonte real: `pkg/channels/`, `pkg/bus/`, `pkg/media/`, `pkg/identity/`,
`cmd/kuromatsu/internal/gateway/`.

## Canais que existem hoje

Só estes quatro estão implementados e registrados (confirmado em
`pkg/channels/`, 2026-09-21 — qualquer lista maior é de uma versão anterior
do fork/upstream):

| Sub-pacote | Nome registrado | Interfaces opcionais que implementa |
|---|---|---|
| `pkg/channels/telegram/` | `"telegram"` | `TypingCapable`, `PlaceholderCapable`, `MessageEditor`, `MediaSender` |
| `pkg/channels/whatsapp/` | `"whatsapp"` | — (modo bridge, via URL de bridge externa) |
| `pkg/channels/whatsapp_native/` | `"whatsapp_native"` | — (modo nativo whatsmeow, conecta direto ao WhatsApp) |
| `pkg/channels/pico/` | `"pico"` | `TypingCapable`, `PlaceholderCapable`, `MessageEditor`, `MediaSender`, `WebhookHandler` |

O Manager escolhe entre `whatsapp`/`whatsapp_native` conforme
`WhatsAppConfig.UseNative`. O protocolo Pico é um WebSocket próprio do
Kuromatsu, recebido via webhook (`WebhookPath` retorna `/pico/`).

## Fluxo de mensagem

```
Canal (Telegram/WhatsApp/Pico) --PublishInbound()--> MessageBus --> AgentLoop
AgentLoop --PublishOutbound()--> MessageBus --SubscribeOutbound()--> Manager
Manager --> channelWorker (fila por canal, rate limit) --> channel.Send()
```

- `pkg/bus/bus.go`: `MessageBus` com 3 canais Go internos (inbound, outbound,
  outboundMedia), buffer 64 cada. `Close()` sinaliza fechamento e drena, mas
  **não fecha os canais Go em si** (evita panic de send-on-closed com
  publishers concorrentes ainda em voo).
- `pkg/bus/types.go`: tipos estruturados — `Peer{Kind, ID}`,
  `SenderInfo{Platform, PlatformID, CanonicalID, Username, DisplayName}`,
  `InboundMessage`, `OutboundMessage`, `OutboundMediaMessage`, `MediaPart`.
  `Metadata map[string]string` é só para extensões específicas do canal
  (ex.: `reply_to_message_id` do Telegram) — peer, message ID e sender já são
  campos de primeira classe, não vão em `Metadata`.

## `BaseChannel` (`pkg/channels/base.go`)

Toda implementação de canal embute `*channels.BaseChannel`, que dá:

- `IsRunning()`/`SetRunning(bool)` (atômico) — chamar `SetRunning(true)` no
  fim de um `Start` bem-sucedido, `SetRunning(false)` no início de `Stop`, e
  checar `IsRunning()` em `Send` (devolvendo `ErrNotRunning` se falso).
- `IsAllowed`/`IsAllowedSender` — checagem de allow-list (delega a
  `pkg/identity`).
- `HandleMessage(...)` — caminho único de entrada: checa permissão, monta
  `MediaScope`, **dispara automaticamente** `TypingCapable`/
  `ReactionCapable`/`PlaceholderCapable` via type assertion no `owner`
  injetado pelo Manager, e publica no bus. Um canal novo não chama essas três
  coisas manualmente — só implementa a interface, se o protocolo suportar.
- Opções funcionais no construtor: `WithMaxMessageLength(n)`,
  `WithGroupTrigger(cfg)`, `WithReasoningChannelID(id)`.

## Registro de fábrica (`pkg/channels/registry.go`)

```go
channels.RegisterFactory(config.ChannelTelegram, func(name, typ string, cfg *config.Config, b *bus.MessageBus) (channels.Channel, error) {
    // decodifica cfg.Channels[name].GetDecoded(), type-assert, constrói
})
// ou, com menos boilerplate:
channels.RegisterSafeFactory(config.ChannelTelegram, NewTelegramChannel)
```

Cada sub-pacote se registra no próprio `init()`; o Gateway dispara isso com
um blank import (`_ "github.com/andre25costa-code/kuromatsu/pkg/channels/telegram"`
em `cmd/kuromatsu/internal/gateway/helpers.go`). O Manager só faz
`InitChannelList()` (decodifica settings por tipo) e busca a fábrica pelo
nome — nenhuma mudança no Manager é necessária para adicionar um canal.

## Classificação de erro e retry (`pkg/channels/errors.go`, `errutil.go`)

`Send` deve devolver (ou envolver) um destes sentinels — o Manager decide o
retry por `errors.Is`, não por inspeção de string:

| Erro | Significado | Retry do Manager |
|---|---|---|
| `ErrNotRunning` | canal parado | não tenta de novo |
| `ErrSendFailed` | falha permanente (ex.: 4xx) | não tenta de novo |
| `ErrRateLimit` | limitado (ex.: 429) | espera 1s, tenta de novo |
| `ErrTemporary` | falha transitória (ex.: 5xx, rede) | backoff exponencial 500ms·2^n, teto 8s |

`ClassifySendError(statusCode, err)` e `ClassifyNetError(err)` fazem essa
tradução a partir de um código HTTP ou erro de rede. Máximo de 3 tentativas
em `sendWithRetry`.

## Orquestração (`pkg/channels/manager.go`)

- Uma `channelWorker` (fila de texto + fila de mídia, cada uma com
  `*rate.Limiter` próprio) por canal ativo.
- `channelRateConfig` só tem uma entrada explícita hoje: `"telegram": 20`
  msg/s. Qualquer outro canal usa o default de 10 msg/s (`burst =
  max(1, ceil(rate/2))`).
- `StartAll`: inicia cada canal → cria o worker → sobe
  `runWorker`/`runMediaWorker`/`dispatchOutbound`/`dispatchOutboundMedia`/
  `runTTLJanitor` (limpeza a cada 10s) → sobe o HTTP server compartilhado
  (se configurado).
- `StopAll`: para o HTTP server (timeout 5s) → cancela o contexto do
  dispatcher → fecha e drena as filas → chama `channel.Stop` em cada canal.
- Typing/Reaction/Placeholder são três pipelines independentes, guardados em
  `sync.Map`s separados (`typingStops`, `reactionUndos`, `placeholders`),
  com TTL próprio (5min/5min/10min) — o `preSend` do Manager consome isso
  antes de mandar a resposta final: para o Typing, desfaz a Reaction, e
  tenta editar o Placeholder no lugar de mandar uma mensagem nova (se o
  canal implementa `MessageEditor`).

## `pkg/media/store.go` — `MediaStore`

`FileMediaStore`: mapeamento em memória (`media://<uuid>` → caminho local),
sem cópia/move de arquivo. Operação em duas fases (coleta sob lock, apaga do
disco sem lock) para minimizar contenção. Limpeza por TTL roda em segundo
plano via `NewFileMediaStoreWithCleanup`.

**Limitação conhecida**: a chamada `ReleaseAll(msg.MediaScope)` no loop do
agente (`pkg/agent/agent.go`) está comentada — limpeza só acontece por TTL,
não por fim de sessão, porque o limite de uma "sessão" para esse propósito
ainda não está bem definido. Não é um bug esquecido; é uma decisão adiada.

## `pkg/identity/identity.go` — identidade canônica

`BuildCanonicalID("telegram", "123456")` → `"telegram:123456"`.
`MatchAllowed` aceita 4 formatos numa allow-list: `"123456"` (PlatformID),
`"@alice"` (Username), `"123456|alice"` (legado, ou/ou), e
`"telegram:123456"` (canônico, match exato).

## Como adicionar um canal novo

1. Criar `pkg/channels/<nome>/{init.go,<nome>.go}` — `init.go` chama
   `channels.RegisterFactory`/`RegisterSafeFactory`; `<nome>.go` embute
   `*channels.BaseChannel` e implementa `Start`, `Stop`, `Send`,
   `IsRunning` (herdado), `IsAllowed`/`IsAllowedSender` (herdado),
   `ReasoningChannelID` (herdado).
2. Implementar só as interfaces opcionais que o protocolo realmente suporta
   (`MediaSender`, `TypingCapable`, `ReactionCapable`, `MessageEditor`,
   `WebhookHandler`, `HealthChecker`) — o resto já é tratado pelo
   `BaseChannel`/Manager.
3. Registrar o tipo de settings em `channelSettingsFactory`
   (`pkg/config/config_channel.go`) e adicionar o blank import em
   `cmd/kuromatsu/internal/gateway/helpers.go`.
4. Testar com `go test ./pkg/channels/<nome>/... -v`.

Convenção de config: cada entrada de `channel_list` tem campos comuns no
nível superior (`enabled`, `type`, `allow_from`) e o específico do canal
dentro de `settings`; segredos (token, senha) vão em `.security.yml`, nunca
em `config.json`.

## Convenções obrigatórias

1. Erro de `Send` **tem** que ser um sentinel (ou envolvê-lo) — erro não
   classificado cai no backoff exponencial genérico, tratado como
   desconhecido.
2. `SetRunning` é sinal de ciclo de vida — sempre `true` no fim de `Start`,
   `false` no início de `Stop`, checado em `Send`.
3. Não duplicar a checagem de permissão antes de `HandleMessage` — ele já
   chama `IsAllowedSender`/`IsAllowed` internamente.
4. Quebra de mensagem longa é responsabilidade do Manager
   (`SplitMessage`, `pkg/channels/split.go`) — o canal só declara o limite
   via `WithMaxMessageLength`. A quebra prefere newline, depois
   espaço/tab, e detecta bloco de código aberto (` ``` `) para não cortar no
   meio: estende até caber o fechamento, ou reabre o bloco depois do corte.
5. Registro de fábrica é sempre em `init()`, disparado por blank import no
   Gateway.

## Testes existentes

`pkg/channels/{base,manager,split,errors,errutil}_test.go`. Para um canal
novo: `go test ./pkg/channels/<nome>/ -v`.
