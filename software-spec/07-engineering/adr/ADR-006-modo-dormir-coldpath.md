---
id: ADR-006
title: Modo dormir sobre evolution.ColdPathRunner; "inconsciente" via fallback chain
status: superseded
superseded_by: ADR-018
version: 3
owner: André
last_updated: 2026-09-11
depends_on: [ADR-002, ADR-008]
---

# ADR-006 — Modo dormir sobre o ColdPathRunner

> **Superseded parcialmente por [ADR-018](ADR-018-sono-evolucao-modelo-externo-obrigatorio.md)
> em 2026-09-11** (`proposed`, aguardando confirmação do André) — **apenas a cláusula 2
> da Decisão abaixo** ("`sleep.unconscious_model`… qualquer provider serve, inclusive o
> próprio `bonsai-local`. Vazio ⇒ chain padrão do agente") deixa de valer: no alvo
> `demetrius`, `unconscious_model` passa a ser **obrigatório** e não pode resolver para
> `provider: native`. As cláusulas 1, 3 e 4 (agendador espelhando `evolution_bridge`,
> pipeline coleta→triagem→aplicação, guardrails BR-001/BR-005/BR-006) **continuam em
> vigor sem mudança** e são a base sobre a qual a ADR-018 constrói (C5 do plano de
> refatoração `demetrius`) — não reimplementar o agendador nem o pipeline a partir da
> ADR-018, só a validação do modelo.

## Contexto
O "modo dormir" é uma rodada noturna de consolidação de memória (poda + reforço),
possivelmente executada por um modelo mais forte ("inconsciente") quando houver chave.
O PicoClaw já tem: `pkg/evolution/cold_path_runner.go` (runner assíncrono single-flight
com coalescing) e um timer agendado em `evolution_bridge.go`; e o `pkg/cron` que executa
*turns de agente* visíveis ao usuário.

## Decisão
1. **Agendador**: clonar o padrão do `evolution_bridge` (timer in-process por janela
   `"HH:MM-HH:MM"`), **não** usar `pkg/cron` — o sono é infraestrutura: deve rodar sem
   canal ativo, não pode ser apagável via `cron rm`, e quer as semânticas do
   `ColdPathRunner` (single-flight/coalescing). `pkg/sleep.Runtime` implementa o método
   `RunColdPathOnce(ctx, workspace) error` e é passado direto a
   `evolution.NewColdPathRunner(runtime)` — Go satisfaz a interface não-exportada do
   parâmetro estruturalmente, então **nenhuma mudança em `pkg/evolution` é necessária**
   (correção desta ADR v1: a exportação prevista não é preciso; confirmado por
   compilação em `pkg/sleep`).
2. **Inconsciente**: `sleep.unconscious_model` é uma ref da `model_list`, resolvida por
   `resolveModelCandidates` + `ExecuteCandidate` — qualquer provider serve, inclusive o
   próprio `bonsai-local`. Vazio ⇒ chain padrão do agente.
3. **Pipeline**: coleta (sessões JSONL + seahorse + `.learnings/`) → triagem LLM
   map-reduce com teto de tokens (BR-006) → aplicação (MEMORY.md atômico, compactação
   seahorse, relatório `sleep-report-<data>.md`). `dry_run` só escreve o relatório.
4. **Guardrails**: desligado por padrão (BR-005); nunca concorrente com turn ativo
   (BR-001); `weekly_deep` aos domingos faz a reorganização completa.

## Consequências
- Zero mudanças em `pkg/evolution`; `pkg/sleep` é testável com `ChatFunc` e
  `SessionSource` fakes, sem depender de `pkg/memory`/`pkg/seahorse`/`pkg/evolution`
  diretamente (o mesmo padrão de adaptador usado em `pkg/providers/localllm`).
- Com o Bonsai como inconsciente, a máquina "sonha com os próprios pesos" às 3h —
  qualidade menor, custo zero; com chave, triagem melhor.
