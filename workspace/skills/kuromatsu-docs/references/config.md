# Configuração

Fonte real: `pkg/config/` (schema principal em `config.go`, `config_struct.go`).

## Onde a config mora

- **`config.json`** — a config principal. Local: `$KUROMATSU_CONFIG` se
  setado; senão `<GetHome()>/config.json`. `GetHome()` usa `KUROMATSU_HOME`
  se setado, senão o diretório default (`~/.kuromatsu` fora do Windows).
  **O diretório de trabalho (workspace) não substitui essa seleção** — uma
  cópia de `config.json` dentro do workspace nunca é a config ativa. Um
  `config.json` obsoleto dentro da pasta do workspace confunde justamente
  por parecer autoritativo sem ser.
- **`.security.yml`** — chaves de API e outros segredos, indexados pelo
  `model_name` do `model_list` (nunca em `config.json`).
- `Config.SourcePath` (campo não persistido, populado por `LoadConfig`)
  guarda o caminho absoluto do arquivo realmente carregado — é o que
  `/show config` reporta, e o que o gateway loga no boot
  (`"Active configuration"`).

`LoadConfig` também avisa (não falha) se existir um `config.json` dentro do
workspace default que **não** é o arquivo ativo — sinal de uma cópia
obsoleta esquecida.

## Variáveis de ambiente

Todo campo com tag `env:"KUROMATSU_..."` em `pkg/config/*.go` pode ser
setado por variável de ambiente, sempre no padrão
`KUROMATSU_<CAMINHO_EM_MAIUSCULO>_<CAMPO>` (ex.: `Agents.Defaults.Workspace`
→ `KUROMATSU_AGENTS_DEFAULTS_WORKSPACE`; alguns blocos usam `envPrefix` no
struct tag em vez de repetir o prefixo campo a campo, ex.: subturn). Hoje
existem 130 dessas variáveis — **não copiadas aqui uma a uma de propósito**:
uma lista hardcoded ficaria desatualizada no primeiro campo novo. Para a
lista exaustiva e sempre correta:

```bash
grep -oE 'env:"KUROMATSU_[A-Z0-9_]+"' pkg/config/*.go | sed 's/.*env:"//;s/"//' | sort -u
```

As mais usadas no dia a dia: `KUROMATSU_HOME`, `KUROMATSU_CONFIG`,
`KUROMATSU_MODELS_DIR`, `KUROMATSU_AGENTS_DEFAULTS_{WORKSPACE,MODEL_NAME,PROVIDER}`,
`KUROMATSU_CHANNELS_TELEGRAM_TOKEN`, `KUROMATSU_CHANNELS_WHATSAPP_*`,
`KUROMATSU_GATEWAY_{HOST,PORT}`.

**Compat legada**: env vars `PICOCLAW_*` (nome anterior do fork) continuam
honradas como shim quando a variável `KUROMATSU_*` equivalente não está
setada (`pkg/config/env_compat.go`).

## Validações que rodam no `LoadConfig`

Além do parse/merge normal, `LoadConfig` roda (nesta ordem, cada uma pode
falhar o boot):

- `ValidateSleep` — `sleep.unconscious_model` (se `sleep.enabled`) e
  `evolution.model` (se a evolução usa cold path) têm que resolver para um
  provider **não-nativo** (decisão de projeto: o sono e a evolução nunca
  rodam no modelo nativo de 1 bit) — ver `sleep.md`.
- `ValidateAgentLoopGuards` — `agents.defaults.max_repeated_tool_calls`
  tem que ser `0` (desligado) ou `>= 2`; com `1` (ou negativo) o boot falha
  com `agents.defaults.max_repeated_tool_calls must be 0 (off) or >= 2`.
- `ValidatePlatformPaths` — rejeita um caminho absoluto estilo Windows
  (`C:\...`, `\\...`) num workspace configurado quando o processo está
  rodando em SO não-Windows, em vez de falhar de forma confusa depois.
- `ValidateFocus` — nomes de janela referenciados em `origins`/`escalate_to`
  existem de fato (ver `scheduling.md`).
- `ValidateTimezone` — `timezone`, se não vazio, tem que ser um nome IANA
  que `time.LoadLocation` conheça; senão o boot falha com
  `timezone "<valor>": ...`. Só valida: o `LoadConfig` nunca altera
  `time.Local`.

## Fuso horário (`timezone`)

- Campo no topo do config: `"timezone": "America/Sao_Paulo"` (env
  `KUROMATSU_TIMEZONE`). Vazio (default) = usa `TZ` ou o fuso do sistema.
- O assistente do `onboard` pergunta o fuso no passo 5 (detecção em
  [install.md](install.md)).
- Aplicado **uma vez por processo** (`config.ApplyProcessTimezone`, com
  `sync.Once`) no primeiro carregamento de cada comando
  (`internal.LoadConfig`) e no boot do gateway, antes de iniciar os
  serviços. Os reloads do gateway não reaplicam (escrever em `time.Local`
  com goroutines rodando seria data race): **mudar `timezone` exige
  reiniciar**.
- `TZ` definido no ambiente (ex.: `Environment=TZ=` num unit do systemd)
  tem precedência sobre o campo.
- No Windows o binário embute `time/tzdata` (~400 KB), porque o sistema não
  tem a base zoneinfo; Linux, macOS e Android usam a do sistema.

## Predefinições de permissões

O assistente do `onboard` (passos em [install.md](install.md)) e a tela de
permissões da web UI gravam uma destas predefinições (`ApplyPermissions` em
`pkg/setup`):

| Chave | `seguro` | `assistente` | `avancado` |
|---|---|---|---|
| `agents.defaults.restrict_to_workspace` | true | true | true |
| `tools.exec.enabled` | false | false | true |
| `tools.exec.allow_remote` | false | false | false |
| `tools.web.enabled` | false | true | true |
| `tools.cron.enabled` | false | true | true |
| `tools.cron.allow_command` | false | false | true |

- Nenhuma liga `allow_remote`: o exec só roda em canais internos
  (cli/system/subagent). Liberar no Telegram é edição manual.
- `setup.MatchPermissions` devolve a predefinição cujos 6 valores batem, ou
  `custom`. O `DefaultConfig()` bate exatamente com `avancado` (exec ligado
  só em canais internos, cron com comandos).
- Numa config nova, Enter no passo 6 escolhe `assistente` (exec desligado);
  o `--non-interactive` mantém o `DefaultConfig()` (= `avancado`).

## Web UI (`webui`)

Página local de configuração servida pelo gateway em `/ui/` (API JSON em
`/api/ui/`), no mesmo listener dos webhooks.

| Chave | Default | Env | Onde |
|---|---|---|---|
| `webui.enabled` | `true` | `KUROMATSU_WEBUI_ENABLED` | `config.json` |
| `webui.allow_remote` | `false` | `KUROMATSU_WEBUI_ALLOW_REMOTE` | `config.json` |
| `webui.token` | criado pelo `onboard` ou por `kuromatsu ui token` | `KUROMATSU_WEBUI_TOKEN` | só no `.security.yml` |

- **Token:** 32 bytes aleatórios em hex. Nasce só no `onboard` (quando falta)
  ou no `kuromatsu ui token` (cria se faltar e imprime). O gateway nunca grava
  o config: sem token ele não monta a UI e registra `web UI disabled: no
  token; run 'kuromatsu ui token'`. `kuromatsu ui open` imprime a URL.
- **Acesso:** com `allow_remote: false`, 403 para quem não é loopback
  (`::ffff:127.0.0.1` conta), para qualquer cabeçalho de proxy
  (`X-Forwarded-For`, `X-Forwarded-Host`, `X-Forwarded-Proto`, `Forwarded`,
  `X-Real-IP`, `CF-Connecting-IP`, `True-Client-IP`) e para `Host` fora de
  `localhost`/`127.0.0.1`/`[::1]` (qualquer porta, então
  `ssh -L 8080:localhost:18790` funciona). Um túnel que não acrescenta
  cabeçalhos (`ssh -R`, `socat`) chega como loopback e aí só o token protege:
  não exponha a porta inteira do gateway. Com `allow_remote: true` o gateway
  avisa no boot e só essas checagens caem; token, Origin e CSRF continuam.
- **Login:** `POST /api/ui/login {token}` cria a sessão (cookie `km_session`,
  HttpOnly, SameSite=Strict, 12 h deslizantes, em memória: restart pede novo
  login) e devolve o token CSRF. Scripts usam `Authorization: Bearer <token>`.
  5 erros em 1 min por IP dão 429 até o minuto acabar (em loopback isso vale
  para todo processo local; o Bearer não é limitado).
- **CSRF/Origin:** com o cookie, toda mudança exige `Origin` (ou `Referer`)
  igual a `http://<Host>` e o cabeçalho `X-CSRF-Token` da sessão. Sem CORS.
- **Gravação:** cada mudança carrega o arquivo como está (`LoadConfigForEdit`,
  sem valores do ambiente), valida com `config.ValidateForSave`, grava e pede o
  mesmo reload do `/reload` (reload já em curso → 202, mudança salva).
  Inválido → 422 e nada gravado. `allow_from` vazio num canal ligado é
  recusado. Chave de modelo em branco mantém a atual; chaves nunca voltam
  nas respostas.
- **Arquivos do workspace:** só `SOUL.md`, `USER.md`, `AGENT.md` e
  `HEARTBEAT.md`, até 256 KB.
- Com os valores padrão (`enabled: true`, `allow_remote: false`) o bloco
  `webui` não é gravado no `config.json` (`WebUIConfig.IsZero` + `omitzero`),
  então um binário anterior a ele ainda carrega o arquivo. Um `enabled: false`
  explícito é gravado.
- `webui.enabled`, `webui.allow_remote` e o token só valem no próximo start.
  Em máquina compartilhada, use `webui.enabled: false`.
- **Equivale a um shell:** o token e uma sessão logada podem ligar a execução
  de comandos (tela de permissões); trate o token como senha. Cookies não são
  isolados por porta: outros apps web locais abertos no mesmo navegador
  (`http://localhost:<outra porta>`) podem receber o cookie da sessão. Em
  máquina que roda apps web locais não confiáveis, deixe a UI desligada ou
  saia (Sair) ao terminar.

## Guardas do loop do agente

Três limites de recursos no loop do agente, todos em `agents.defaults`:

| Chave | Default | Env | O que faz |
|---|---|---|---|
| `max_repeated_tool_calls` | `3` (`0` desliga; `1` é inválido) | `KUROMATSU_AGENTS_DEFAULTS_MAX_REPEATED_TOOL_CALLS` | Quando a mesma tool é chamada com os mesmos argumentos e devolve o mesmo resultado N-1 vezes seguidas, a N-ésima chamada não executa: o modelo recebe um aviso. Se insistir, o turno termina com uma mensagem pedindo para reformular. Polling cuja saída muda nunca conta como repetição. |
| `user_turn_max_minutes` | `30` (`0` = sem teto) | `KUROMATSU_AGENTS_DEFAULTS_USER_TURN_MAX_MINUTES` | Teto de tempo de um turno de usuário inteiro (mensagem de canal ou CLI, incluindo o dreno das mensagens de steering enfileiradas). No estouro, o usuário recebe `Error processing message: turn exceeded agents.defaults.user_turn_max_minutes (<N> min)`. Cron, heartbeat e sistema não usam este teto: usam `non_user_turn_max_minutes` (default 60). |
| `subturn.max_concurrent_global` | `8` (`<= 0` usa o default) | `KUROMATSU_AGENTS_DEFAULTS_SUBTURN_MAX_CONCURRENT_GLOBAL` | Máximo de sub-turnos simultâneos **por nível de profundidade** no processo inteiro (além do limite por pai, `subturn.max_concurrent`). Com os defaults (profundidade 3), no máximo 24 sub-turnos rodam ao mesmo tempo. Um filho que não consegue vaga em `subturn.concurrency_timeout_sec` falha com timeout de concorrência. Com memória apertada, reduza. |

O teto de 30 min vale também na CLI e com o modelo nativo, que é lento: um
turno legítimo muito longo é cortado. Para desligar, use
`"user_turn_max_minutes": 0`.

O teto não interrompe um sub-turno síncrono em andamento: se o prazo vence
enquanto o turno espera um filho, ele só retorna quando o filho termina, o
que é limitado por `subturn.default_timeout_minutes`. Logo, 30 min não é um
limite rígido nesse caso.

Sub-turnos herdam o contexto do processo (o de `Run`), não o do turno pai:
continuam depois que o pai termina, mas param no desligamento.

**Exige reinício:** `user_turn_max_minutes`, `subturn.max_concurrent_global`
e `subturn.max_depth` (que dimensiona os pools por nível) são lidos uma vez,
na partida. O reload de config em execução não os aplica; reinicie o
processo depois de mudá-los. `max_repeated_tool_calls` vale já no próximo
turno.

## Perfis de hardware

Não existe campo `profile` no config. Um perfil é um conjunto de valores
de chaves que já existem, gravado uma vez como valores explícitos:

- no `kuromatsu onboard` de uma config nova, com o perfil detectado;
- com `kuromatsu onboard --profile <ultra-light|light|balanced|performance|auto>`
  numa config existente (`auto` = o detectado);
- no passo 4 do assistente, escolhido explicitamente. Sem isso, uma config
  existente não muda.

As faixas de RAM/CPU de cada perfil, a detecção e o `kuromatsu status`
estão em [install.md](install.md#perfis-de-hardware). Valores gravados:

| Chave | Ultra-Light | Light | Balanced | Performance |
|---|---|---|---|---|
| `extra_body.n_ctx` do modelo nativo (`bonsai-local`) | 1024 | 2048 | 4096 | 8192 |
| `extra_body.n_threads` do modelo nativo | min(CPUs,2) | min(CPUs,4) | min(CPUs,4) | min(CPUs,8) |
| `memguard.min_available_mb` | 128 | 256 | 512 | 1024 |
| `memguard.margin_mb` | 64 | 96 | 96 | 128 |
| `agents.defaults.subturn.max_depth` | 2 | 3 | 3 | 3 |
| `agents.defaults.subturn.max_concurrent` | 2 | 5 | 5 | 8 |
| `agents.defaults.subturn.max_concurrent_global` | 2 | 8 | 8 | 16 |
| `heartbeat.interval` (min) | 60 | 30 | 30 | 15 |

- **Light = defaults atuais.** Aplicar Light numa config padrão não muda
  nada além de explicitar os limites de sub-turno (3/5/8).
- `n_threads` só é gravado quando difere do default do motor nativo,
  `min(CPUs,4)`; senão a chave é removida, para a config não fixar o número
  de CPUs da máquina onde foi criada.
- Os limites do memguard iguais ao default (256/96) ficam sem valor (0 =
  default).
- **`memguard.min_available_mb`/`margin_mb` só têm efeito com
  `memguard.enabled: true`.** Os perfis não ligam o memguard (nem mexem em
  `heartbeat.enabled`, modelos, canais ou permissões).
- O `extra_body` do nativo só é tocado se a entrada `bonsai-local` existir
  em `model_list`; o perfil nunca cria a entrada.
- **Limite conhecido do `n_ctx` pequeno:** o prompt do primeiro turno de um
  workspace novo, com todas as tools, tem cerca de 2,9 mil tokens. Isso não
  cabe em `n_ctx` ≤ 2048 (Ultra-Light, Light e o default do motor nativo).
  Com o modelo nativo como **primário** nesses perfis, o primeiro turno
  estoura o contexto; como fallback de um modelo externo, ele só é usado
  quando o externo falha. Para usar o nativo como primário, aumente o
  `n_ctx` onde a RAM permitir ou restrinja as tools (janelas de foco, ver
  [scheduling.md](scheduling.md), ou `tools` no `AGENT.md`, ver
  [workspace.md](workspace.md)).
- **CPU pinning/affinity:** não suportado; use `taskset` ou o
  `CPUAffinity=` do systemd fora do Kuromatsu.

## Estrutura de alto nível do `Config`

Blocos principais (todos em `pkg/config/config.go`): `Agents` (`Defaults` +
`List` — ver abaixo, e `Dispatch`), `ModelList`, `Channels`/`Session`,
`Tools` (exec, cron, web, skills, sysmon, MCP...), `Runstate`, `Memguard`,
`Telemetry`, `Sleep`, `Evolution`, `Heartbeat`, `Gateway`, `WebUI`, `Isolation`.

## Multi-agente (`agents.list`, `agents.dispatch`)

`agents.list` vazio (o caso comum) é equivalente a um único agente
implícito `main`, usando `agents.defaults` inteiro — nenhum comportamento
muda. Com `agents.list` preenchido, cada entrada pode sobrescrever
`workspace`/`model`/`skills`/`heartbeat` campo a campo (fallback pro
default quando omitido); `agents.dispatch.rules` roteia por
canal/conta/chat para um `agent` específico. Precedência: pin `/agent` >
regra de dispatch > default; cada agente roda numa `AgentInstance` própria
(decisão de projeto: multi-agente é opt-in e, sem `agents.list`, nada muda).

## Migração de config antiga

Única migração hoje: `~/.picoclaw` → `~/.kuromatsu` (leitura in-place, sem
reescrever nada) — ver `pkg/migrate/` (decisão de projeto: o rebrand lê o
diretório antigo no lugar, sem cópia, para não gastar disco e RAM numa
máquina pequena).
