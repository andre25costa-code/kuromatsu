# Configuração

Fonte real: `pkg/config/` (schema principal em `config.go`, `config_struct.go`).

## Onde a config mora

- **`config.json`** — a config principal. Local: `$KUROMATSU_CONFIG` se
  setado; senão `<GetHome()>/config.json`. `GetHome()` usa `KUROMATSU_HOME`
  se setado, senão o diretório default (`~/.kuromatsu` fora do Windows).
  **O diretório de trabalho (workspace) não substitui essa seleção** — uma
  cópia de `config.json` dentro do workspace nunca é a config ativa
  (achado real de um audit interno de 2026-09-16: uma cópia obsoleta em
  `workspace/config.json` causou confusão justamente por parecer
  autoritativa sem ser).
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
existem 125 dessas variáveis — **não copiadas aqui uma a uma de propósito**:
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
setada (`pkg/config/env_compat.go`, AC-008-3).

## Validações que rodam no `LoadConfig`

Além do parse/merge normal, `LoadConfig` roda (nesta ordem, cada uma pode
falhar o boot):

- `ValidateSleep` — `sleep.unconscious_model` (se `sleep.enabled`) e
  `evolution.model` (se a evolução usa cold path) têm que resolver para um
  provider **não-nativo** (ADR-018) — ver `sleep.md`.
- `ValidateAgentLoopGuards` — `agents.defaults.max_repeated_tool_calls`
  tem que ser `0` (desligado) ou `>= 2`; com `1` (ou negativo) o boot falha
  com `agents.defaults.max_repeated_tool_calls must be 0 (off) or >= 2`.
- `ValidatePlatformPaths` — rejeita um caminho absoluto estilo Windows
  (`C:\...`, `\\...`) num workspace configurado quando o processo está
  rodando em SO não-Windows, em vez de falhar de forma confusa depois.
- `ValidateFocus` — nomes de janela referenciados em `origins`/`escalate_to`
  existem de fato (ver `scheduling.md`).

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

- no `kuromatsu onboard` de uma config nova, com o perfil detectado (uma
  linha `Hardware: ... → profile <nome> (applied)` mostra qual);
- com `kuromatsu onboard --profile <ultra-light|light|balanced|performance|auto>`
  numa config existente (`auto` = o detectado). Sem `--profile`, uma config
  existente não muda.

O perfil é escolhido pela RAM efetiva (a menor entre a RAM total e o limite
do cgroup) e pelo número de CPUs, nesta ordem:

| Perfil | Condição |
|---|---|
| Ultra-Light | RAM < 768 MiB |
| Light | RAM < 2048 MiB |
| Balanced | RAM < 8192 MiB, ou menos de 4 CPUs |
| Performance | o resto |
| (RAM não detectada) | Light |

Valores gravados:

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
- `kuromatsu status` mostra o hardware detectado, o perfil sugerido e a
  qual perfil a config corresponde (`custom` se nenhum bater; um campo
  zerado conta como o default).
- **CPU do nativo:** o build nativo exige AVX2+FMA (x86-64) ou
  dotprod+fp16 (arm64). Numa CPU sem isso, o modelo nativo não é ligado
  como default nem como fallback, uma carga explícita é recusada com erro, e
  o gateway registra um aviso no boot.
- **CPU pinning/affinity:** não suportado; use `taskset` ou o
  `CPUAffinity=` do systemd fora do Kuromatsu.

## Estrutura de alto nível do `Config`

Blocos principais (todos em `pkg/config/config.go`): `Agents` (`Defaults` +
`List` — ver abaixo, e `Dispatch`), `ModelList`, `Channels`/`Session`,
`Tools` (exec, cron, web, skills, sysmon, MCP...), `Runstate`, `Memguard`,
`Telemetry`, `Sleep`, `Evolution`, `Heartbeat`, `Gateway`, `Isolation`.

## Multi-agente (`agents.list`, `agents.dispatch`)

`agents.list` vazio (o caso comum) é equivalente a um único agente
implícito `main`, usando `agents.defaults` inteiro — nenhum comportamento
muda. Com `agents.list` preenchido, cada entrada pode sobrescrever
`workspace`/`model`/`skills`/`heartbeat` campo a campo (fallback pro
default quando omitido); `agents.dispatch.rules` roteia por
canal/conta/chat para um `agent` específico. Ver ADR-019 para o desenho
completo (pin `/agent`, precedência pin > dispatch > default, isolamento
por `AgentInstance`).

## Migração de config antiga

Única migração hoje: `~/.picoclaw` → `~/.kuromatsu` (leitura in-place, sem
reescrever nada) — ver `pkg/migrate/` e ADR-005.
