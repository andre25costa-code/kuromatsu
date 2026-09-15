---
id: ADR-017
title: Guarda de memória dinâmica (memguard) derivada do cgroup + telemetria por turno em SQLite
status: accepted
version: 1
owner: André
last_updated: 2026-09-12
depends_on: [ADR-003, ADR-008]
---

# ADR-017 — Guarda de memória dinâmica + telemetria em SQLite

## Contexto
Em 1 GB de RAM (C1/S06), o limite Go hoje é **estático e descolado das reservas C**:
`GOMEMLIMIT=650MiB` fixo, enquanto o agente passa `max_tokens=32768` e
`ContextWindow=131072` para um engine cujo `n_ctx` real é `2048` — nada bate. O
diagnóstico de "malloc manual vs GC do Go" levantado na sessão está parcialmente
correto: KV/compute já são `malloc` do `ggml` (C), o Go não faz malloc — o risco real
não é falta de controle sobre alocação C, é esse limite Go estático colidir com o OOM
killer do kernel sem nenhuma coordenação com o que o lado C está de fato reservando.

Separadamente, o André pediu telemetria por turno (ideia original: "álgebra
linear/regressão em C" para um "vetor de produtividade") — o mesmo insight (dados reais
para calibrar janelas e o modo dormir) cabe em agregação SQL, sem C novo e testável no
Windows sem cgo.

## Decisão
1. **`pkg/runstate/memguard{,_linux,_other}.go`**: `Readers` (MemInfo, CgroupLimit,
   PSI, ProcessRSS) extraídos de `pkg/tools/sysmon_linux.go:50-137,190` para um
   `pkg/sysinfo` compartilhado — o `sysmon` (FR-011) não muda de comportamento, só
   passa a compartilhar os parsers.
2. **`Plan(est) = min(cgroup, MemTotal) − modelo − KV − compute − margem`** (clamp
   ≥128 MiB) alimenta `debug.SetMemoryLimit` dinamicamente no `postLoad` do engine
   (+ `debug.SetGCPercent(50)`), substituindo o `GOMEMLIMIT` fixo por um valor
   derivado da carga real. Na `demetrius`: `969 − 237 (modelo) − 119 (KV) − 96
   (compute) − 96 (margem) ≈ 420 MB` de limite Go, sob `MemoryMax=850M` do systemd
   (ADR-013) — ~300 MB de folga.
3. **`PreLoad`**: nega carregar o modelo se `MemAvailable < KV + compute + margem`,
   com erro de texto `overloaded` — a classificação de erro existente mapeia isso para
   `FailoverOverloaded` (retry transitório já existente em
   `pipeline_llm.go:339-367`), sem precisar de lógica nova de retry.
4. **Vigilante PSI** (`/proc/pressure/memory`) em `Run(ctx)`: `some>20` → `GC()` +
   `debug.FreeOSMemory()` (no máximo 1×/min); `full>10%` sustentado por 30s →
   `Suspend()` (runstate, ADR-016) + `localllm.UnloadAll()` — descarrega o modelo
   **antes** do OOM killer agir; recupera (`Resume()`) com `full<5%` por 30s.
5. **Sem Linux/PSI** (ex. desenvolvimento no Windows): memguard desliga com aviso,
   `GOMEMLIMIT` estático continua sendo honrado — nenhuma regressão.
6. **Telemetria por turno em SQLite**: tabela `turns` (modo WAL,
   `$KUROMATSU_HOME/telemetry/turns.db`), usando `modernc.org/sqlite` — **o mesmo
   driver puro-Go do seahorse** (ADR-008), sem cgo adicional. `Recorder` assíncrono
   (canal de 256, nunca bloqueia o turno; descarta com contador se saturar). Campos
   incluem `steal_pct` (delta de `steal` em `/proc/stat` durante o turno — evidencia o
   throttling de créditos do e2-micro, S06 R7) e `escalations`/`unknown_tool_calls`
   (ADR-014). Comando `/stats [janela] [horas]` agrega via SQL puro (contagem, prompt
   médio, % em cache, p50/p90).

## Alternativas
- **Matriz de álgebra linear/regressão em C** para um "vetor de produtividade":
  rejeitada — o mesmo insight (dados reais por janela/hora) cabe em agregação SQL
  sobre a tabela `turns`; zero C novo, testável no Windows sem cgo, e o modo dormir
  (ADR-018) pode consumir os mesmos dados depois.
- **`mlock` do modelo inteiro**: adiado — exigiria aumentar `LimitMEMLOCK` e não
  resolve o problema real identificado (o limite Go estático e descolado das reservas
  C); pode voltar como follow-up se a paginação do mmap se mostrar um problema medido.

## Consequências
- Zero OOM kill esperado em 1 GB (gate de aceite: teste de pressão sintética → GC/
  unload/suspend sem nenhuma linha de OOM em `dmesg`).
- Evidência real por janela/hora (`/stats`) para calibrar janelas (ADR-014) e o modo
  dormir (ADR-018) — sem essa telemetria, ajustar `sticky_turns`/triggers/orçamento de
  tokens seria só palpite.
- Mais uma goroutine de vigilância rodando continuamente — custo de CPU pequeno, mas
  não zero, num orçamento de CPU já apertado no e2-micro (ver ADR-013, S06 R7).
- Sem `memguard.enabled`: `GOMEMLIMIT` estático de hoje continua sendo honrado, sem
  telemetria — nenhuma regressão de comportamento.
