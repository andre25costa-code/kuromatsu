---
id: S30
title: Agendamento — heartbeat, cron e janelas por origem
status: confirmed
version: 2
owner: André
last_updated: 2026-09-12
depends_on: [S09, S11, S16, S17]
---

# S30 — Agendamento (heartbeat, cron)

> **Status**: `confirmed` — ADR-014 (`accepted`), plano `demetrius`. Este item estava
> `missing` ("cron/heartbeat herdados sem mudança... consolidar aqui se os
> agendadores forem alterados"). O agendamento em si (ticker do heartbeat,
> persistência de jobs do cron) **continua sem mudança de mecânica** — o que muda é
> que os dois passam a ter **janela de foco própria** (FR-014) e a CPU da `demetrius`
> passa a ser tratada como orçamento, não como recurso ilimitado (ADR-013, S06/R7).
> Por isso este item sai de `missing`: há conteúdo novo suficiente para registrar,
> mesmo sem uma nova máquina de estados (essa é o S09).

## O que muda: janela própria por origem

O roteador de foco (S16/ADR-014) resolve uma janela **antes** de montar o prompt.
Para origens não-usuário, a janela não vem das regras de keyword/regex — vem direto
de `Origins` (config `focus.origins`):

| Origem | Janela padrão | Tools | Histórico | Aderência | Escala |
|---|---|---|---|---|---|
| `heartbeat` | `heartbeat` | `message`, `sysmon`, `cron` | `off` (`NoHistory`) | — | não (teto é a própria janela) |
| `cron` | `cron` | `exec`, `message` | `off` (`NoHistory`) | — | não |

Sem foco habilitado (`focus.enabled: false`, default fora da `demetrius`), heartbeat
e cron continuam usando o `turn_profile` estático de hoje — **nenhuma mudança de
comportamento** (AC-014-9).

## Orçamento de CPU: heartbeat e cron como consumidores a controlar

A `demetrius` (Google `e2-micro`) entrega 0,25 vCPU sustentado + créditos de burst —
CPU gasta à toa por um agendador é CPU que não sobra para o turno de usuário depois
que os créditos acabam (ver S06/R7). Duas decisões operacionais, tomadas no nível do
plano (não é uma trava de código nova, é orientação de config/uso):

- **Intervalo do heartbeat**: o código (`pkg/heartbeat/service.go:26-28`) já aceita
  qualquer intervalo ≥5 min (`minIntervalMinutes`), com default de 30 min
  (`defaultIntervalMinutes`) quando não configurado. Na `demetrius`, a recomendação
  operacional é **≥60 min** — uma janela `heartbeat` enxuta (poucas tools, sem
  histórico) já reduz o custo por disparo; espaçar os disparos reduz a *frequência*
  do gasto. Isso é config (`heartbeat.interval_minutes` no `config.json`), não um
  novo mínimo no código.
- **Preferir jobs `Command` a jobs `Message` no cron**: `pkg/tools/cron.go` já
  distingue `job.Payload.Command` (execução determinística via `exec`, sem chamar o
  LLM) de `job.Payload.Message` (dispara um turno de agente completo,
  `ExecuteJob`/`agent_message.go`). Um job `Command` custa ~0 tokens; um job
  `Message` paga prefill de qualquer forma (mitigado, não eliminado, pelo cache de
  prefixo — S21). Para lembretes/checagens determinísticas, `Command` é
  estritamente mais barato — orientação a registrar na skill `kuromatsu-docs` (E8),
  não uma restrição imposta pelo `CronTool` (`allow_command` continua gate de
  segurança, não de custo).

## Preempção e "pular, não enfileirar"

Quando `runstate.enabled: true` (S09), o heartbeat instala
`SetShouldSkip(fn)` — checado no topo de `executeHeartbeat`
(`pkg/heartbeat/service.go:149-161`): se `Snapshot().Has(Inference|ToolExec|Dream)`,
o disparo é **pulado e logado**, nunca enfileirado para rodar depois. O próximo
tick tenta de novo do zero. Isso não é fila com backlog — um heartbeat perdido é
só um heartbeat perdido (comportamento aceito, documentado em S17). Cron não tem
essa trava ainda (ADR-016 lista o hook de heartbeat explicitamente; cron continua
correndo o risco normal de concorrência pelo único `llama_context`, mitigado pelo
single-flight existente do provider nativo — ADR-003/015).

## O que não muda

- Persistência de jobs do cron (`cron.CronJob`, schedule, `add/list/get/update/remove`)
  — sem alteração de schema ou de API.
- Lógica de disparo do heartbeat (ticker, `HEARTBEAT.md`, marcador de tarefas do
  usuário, envio da resposta pelo último canal ativo) — sem alteração.
- Intervalo mínimo de 5 min do heartbeat continua sendo um piso de código, não uma
  trava nova — a recomendação de ≥60 min na `demetrius` é operacional/config, não
  imposta pelo `NewHeartbeatService`.

## Cross-referências

- **Tabela completa de janelas** (incluindo `heartbeat`/`cron`) e fluxo de
  roteamento: ADR-014, S16.
- **Estados/refcount que produzem o gate de "ocupado"**: S09. **Retry/skip
  semântico**: S17.
- **`steal_pct`/bit `Throttled` evidenciando o throttling de créditos de burst**:
  S34.
