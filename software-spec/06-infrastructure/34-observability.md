---
id: S34
title: Observabilidade — estado, telemetria e sd_notify
status: confirmed
version: 2
owner: André
last_updated: 2026-09-12
depends_on: [S09, S17, S21, S29]
---

# S34 — Observabilidade

> **Status**: `confirmed` — ADR-016/017 (`accepted`), plano `demetrius`. Este item estava
> `missing` ("observabilidade = logs existentes + sysmon + nativebench. Sem
> métricas/alertas estruturados"). Isso permanece verdade quanto a **alertas** (ainda
> não existem), mas deixa de ser verdade quanto a **estado** e **métricas por
> turno**: o `runstate` (S09) passa a expor estado observável em três canais, e a
> telemetria SQLite (FR-019) passa a registrar métricas estruturadas por turno —
> volume suficiente para um capítulo próprio.

## Estado observável (três canais, uma fonte)

Todos os três leem do mesmo `runstate.Engine.Snapshot()` (S09) — nunca divergem
entre si por construção:

| Canal | Formato | Consumidor típico |
|---|---|---|
| `$KUROMATSU_HOME/run/state` | Texto plano, escrita atômica: `bits=… names=… since=…` | Shell/cron/scripts externos ao binário (`cat`, `watch`) |
| `sd_notify` (`STATUS=<names>`) | Datagrama unix no `$NOTIFY_SOCKET` | `systemctl status kuromatsu` (mostra a última `STATUS=` recebida) |
| `/health` → campo `state:{bits,names}` | JSON, aditivo (sem o `runstate` habilitado, o JSON de hoje não muda) | Monitoramento HTTP, `curl`, dashboards futuros |
| Ação `sysmon state` | Mesma leitura, via tool do agente | O próprio agente, respondendo "o que você está fazendo agora" numa conversa |

`since` (tempo no estado atual) é exposto propositalmente para detectar rápido um
"bit vazado" em produção (S09) — um `Inference` ativo por horas é sinal de bug, não
de um turno legítimo.

## `sd_notify` e o watchdog do systemd

A unit hoje (`deploy/systemd/kuromatsu.service`) é `Type=simple` com um comentário
explícito:

> `TODO(runstate/C1): trocar para Type=notify + NotifyAccess=main + WatchdogSec=120
> quando o publicador sd_notify (pkg/runstate) estiver implementado (ADR-016).`

Ou seja: o watchdog do systemd (`WatchdogSec`, que reinicia o processo se ele parar
de enviar `WATCHDOG=1` periodicamente) **ainda não está ativo** — depende do
publicador `sd_notify` (C1) existir. `SdReady()`/`SdStopping()` (boot/shutdown do
gateway) são o primeiro uso planejado desse canal, antes do watchdog periódico.

## Telemetria por turno (`turns.db`, FR-019)

Tabela `turns` (SQLite, WAL, `$KUROMATSU_HOME/telemetry/turns.db`) — uma linha por
turno concluído (incluindo reflexos, com `prompt_tokens=0`):

`ts, origin, session_key, window, escalations, unknown_tool_calls, prompt_tokens,
cached_tokens, output_tokens, prefill_ms, gen_ms, total_ms, tools_called,
iterations, status, mode_bits, rss_kb, steal_pct`

Dois campos merecem destaque por ligarem diretamente ao que foi medido nesta sessão
na `demetrius`:

- **`cached_tokens`**: evidencia o cache de prefixo (S21) turno a turno — a métrica
  operacional real de "quanto do prompt não precisou ser reprocessado", sem precisar
  ler logs.
- **`steal_pct`**: delta de `steal` (`/proc/stat`) durante o turno. **Importante**:
  o throttling de créditos de burst do e2-micro **não aparece como `steal`** —
  medido nesta sessão (`.claude/team/research/g0-medicao-real.md`): `st=0` em todas
  as amostras, mesmo sob o piso estrangulado (pp64 1,6–2,0 tok/s vs ~7,3 tok/s em
  burst). `steal_pct` continua útil para distinguir *hipervisor* (Oracle, problema
  antigo) de *throttling de burst* (e2-micro, problema novo) — mas sozinho **não**
  detecta o segundo caso; a evidência real do throttling de burst é o próprio tok/s
  medido (`prefill_ms`/`gen_ms` contra `prompt_tokens`/`output_tokens`), não
  `steal_pct`. Registrado aqui para não repetir, em produção, a armadilha desta
  sessão (interpretar `st=0` como "sem problema de CPU").

`/stats [janela] [horas]` agrega isso via SQL puro (contagem, prompt médio, % em
cache, tempo médio, p50/p90) — sem dependência nova, o mesmo driver puro-Go
(`modernc.org/sqlite`) já usado pelo seahorse (ADR-008).

## Logs estruturados (Trilho B0)

Cada chamada de `completion` no engine cgo passa a logar (nível info,
`localllm`/`completion`): `prompt_tokens, cached_tokens, new_tokens, output_tokens,
prefill_ms, gen_ms, gen_tok_s, n_threads`. Isso é o que
`journalctl -u kuromatsu | grep localllm` filtra para depuração manual, e é a fonte
que alimenta as colunas equivalentes de `turns.db` (a telemetria não duplica
instrumentação — consome o `UsageInfo` que o log já registra).

## `nativebench` — banco ad hoc, não telemetria contínua

`cmd/nativebench` continua sendo a ferramenta de medição pontual (baseline S39,
`-n-threads`, `-repeat`) — não é substituído pela telemetria contínua de produção;
os dois convivem com propósitos diferentes (medição controlada vs. observação do
tráfego real).

## O que ainda não existe

- **Alertas** — nada dispara uma notificação proativa quando algo degrada (ex.:
  `Suspended` prolongado, `Throttled` persistente); hoje é preciso consultar
  `/health`/`run/state`/`/stats` ativamente. Fica como possível follow-up, não
  comprometido neste plano.
- **Exportador de métricas** (Prometheus ou similar) — a telemetria é SQLite local,
  consultada por `/stats` ou `sqlite3` direto; não há scrape endpoint.
- Consolidar este capítulo mais a fundo se a observabilidade crescer além do que já
  está aqui (mesmo critério que valia quando o item era `missing`).

## Cross-referências

- **Estados publicados**: S09. **Semântica de degradação que aparece aqui**: S17.
- **`cached_tokens` e a camada de cache que ele evidencia**: S21.
- **Metas quantificadas que a telemetria ajuda a verificar** (turno ≤90s, cache
  ≥70% a partir do 2º turno): S29 (NFR-006/NFR-009).
- **Decisões de arquitetura**: ADR-016 (`runstate`/publicadores), ADR-017
  (memguard/telemetria SQLite), ADR-015 (`cached_tokens` no `UsageInfo`).
