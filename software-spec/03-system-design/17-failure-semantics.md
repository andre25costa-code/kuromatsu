---
id: S17
title: Semântica de falhas e estratégia de consistência
status: confirmed
version: 2
owner: André
last_updated: 2026-09-12
depends_on: [S06, S09, S11, S18]
---

# S17 — Semântica de falhas e estratégia de consistência

> **Status**: `confirmed` — combina comportamento já existente (ADR-002/003, hoje
> registrado em S18) com o comportamento novo do plano `demetrius` (ADR-015/016/017,
> `accepted`). Este item estava `missing` (a semântica de falha do provider nativo
> vivia dispersa em ADR-003/S18); com `runstate` e `memguard` chegando, "o que
> acontece quando algo falha" ganhou volume suficiente para um capítulo próprio.
> **Retry/DLQ aqui é a fonte única** — S24 e S30 só cruzam-referenciam.

## Idempotência

| Operação | Chave de dedup | Observação |
|---|---|---|
| Turno de agente | Nenhuma — turnos não são idempotentes por natureza (cada mensagem gera efeitos) | N/A |
| Reflexo `exec` | Nenhuma — herda o gating existente de `pkg/tools` (deny patterns, restrict) | Mesma regra de hoje, sem mudança |
| Modo dormir / evolução (`Dream`) | `TryEnterDream` — só uma rodada ativa por vez (single-flight herdado de `ColdPathRunner`) | Coalescing já existente; retry re-tenta a mesma rodada, não duplica |
| Escalonamento de janela | `focusEscalations` por turno (não persistido entre turnos) | Reinicia a cada novo turno |

## Retry e backoff

| Situação | Classificação de erro | Retry? |
|---|---|---|
| Overflow de contexto (prompt+64 > `n_ctx`) | `context_length_exceeded` → não-retriable | Não — aciona sumarização do pipeline (existente, ADR-003) |
| Memória insuficiente para carregar o modelo (`PreLoad`, ADR-017) | texto `overloaded` → `FailoverOverloaded` | Sim — retry transitório já existente em `pipeline_llm.go:339-367`, sem lógica nova |
| `TryEnterDream` recusado (`ErrBusy`, S09) | N/A (não é erro de LLM) | Sim, pelo chamador: evolução re-tenta a cada ~2 min; sono re-tenta até o deadline da própria janela |
| Cancelamento de contexto durante prefill (`abort_callback`, ADR-015) | `context.Canceled` | Não é erro — é a intenção do chamador |
| Heartbeat com `runstate` ocupado (S09/S30) | N/A | Não re-enfileira — pula a janela perdida, tenta de novo no próximo tick |

## Compensação

Sem chamadas externas com efeito colateral que precisem de compensação: tool calls são
locais (arquivo, exec, cron) e ações destrutivas (`sysmon kill/renice`) já são negadas
por padrão (BR-007) — não há "desfazer" a implementar, a proteção é preventiva. O único
efeito externo persistente novo é o backup noturno (S33) — se o `rsync` falhar no meio,
o próximo timer tenta de novo com o `tar.zst` da noite seguinte; não há reconciliação
de um backup parcial (aceito, ver S33 para o gap de restore procedure).

## Falha parcial

- **Loop de tool call alucinada**: coberto no fluxo S16 — rejeição imediata para tool
  inexistente no catálogo global (sem escalar), escalonamento bounded a 1×/turno para
  tool existente fora da janela, `denyByTurnProfile` como guarda final. Nunca entra em
  loop porque o teto de 1 escalonamento é absoluto por turno.
- **Rodada de sono/evolução interrompida por inferência de usuário** (S09): o relatório
  de sono registra o corte (herdado do comportamento de `dry_run`/orçamento de tokens
  já existente em FR-010); a próxima janela tenta de novo do zero (sem estado parcial
  persistido entre tentativas).
- **Triagem do modo dormir atinge o orçamento de tokens no meio** (BR-006, já
  existente): a rodada para e o relatório parcial registra o corte — sem mudança nesta
  refatoração, citado aqui por completude.

## Degradação

| Gatilho | Comportamento degradado |
|---|---|
| PSI `some > 20%` (memguard) | GC/`FreeOSMemory` (≤1×/min) — invisível ao usuário, sem interrupção de turno |
| PSI `full > 10%` sustentado ≥30 s (memguard) | `Suspend()` + `UnloadAll()` — chamadas novas recebem `overloaded`/`ErrBusy` até `Resume()` (full<5% por ≥30s); turno em andamento não é abortado à força, só novas entradas são bloqueadas |
| `steal% > 50%` sustentado ≥60 s (e2-micro estrangulado) | Nenhuma ação automática — bit `Throttled` (S09) é só informativo; a UX se degrada em latência (prefill mais lento), não em correção — mitigado por ADR-015 (custo por turno é o delta, não o prompt completo, mesmo no piso) |
| `sleep`/evolução sem modelo externo configurado (ADR-018) | Sono/evolução ficam desligados com log claro; o restante do agente (conversa com o Bonsai nativo) funciona normalmente — não é uma falha, é uma feature opcional inativa |
| Plataforma sem Linux/PSI (ex. desenvolvimento no Windows) | `memguard` desliga com aviso; `GOMEMLIMIT` estático (se configurado) continua sendo honrado |

## Cross-referências

- **Estados e refcount que produzem `ErrBusy`**: S09.
- **Exposição observável de cada estado degradado** (`/health`, logs, `run/state`):
  S34.
- **Retry/DLQ de eventos e jobs em lote**: cruze para este capítulo — não duplicado em
  S24/S30.
