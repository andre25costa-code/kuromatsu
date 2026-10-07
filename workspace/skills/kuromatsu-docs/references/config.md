# Configuração

Fonte real: `pkg/config/` (schema principal em `config.go`, `config_struct.go`).

## Onde a config mora

- **`config.json`** — a config principal. Local: `$KUROMATSU_CONFIG` se
  setado; senão `<GetHome()>/config.json`. `GetHome()` usa `KUROMATSU_HOME`
  se setado, senão o diretório default (`~/.kuromatsu` fora do Windows).
  **O diretório de trabalho (workspace) não substitui essa seleção** — uma
  cópia de `config.json` dentro do workspace nunca é a config ativa
  (achado real do audit de 2026-09-16, ver `docs/internal-audit-2026-09-16.md`:
  uma cópia obsoleta em `workspace/config.json` causou confusão justamente
  por parecer autoritativa sem ser).
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
existem 119 dessas variáveis — **não copiadas aqui uma a uma de propósito**:
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
- `ValidatePlatformPaths` — rejeita um caminho absoluto estilo Windows
  (`C:\...`, `\\...`) num workspace configurado quando o processo está
  rodando em SO não-Windows, em vez de falhar de forma confusa depois.
- `ValidateFocus` — nomes de janela referenciados em `origins`/`escalate_to`
  existem de fato (ver `scheduling.md`).

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
