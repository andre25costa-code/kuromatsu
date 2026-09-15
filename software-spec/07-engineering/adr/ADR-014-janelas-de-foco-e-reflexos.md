---
id: ADR-014
title: Janelas de foco (roteador determinístico por mensagem) + reflexos de 0 tokens
status: accepted
version: 1
owner: André
last_updated: 2026-09-12
depends_on: []
---

# ADR-014 — Janelas de foco + reflexos

## Contexto
O bloqueio nº1 de usabilidade medido no E7: o prompt-base do agente (system + 19
schemas de tools, sem histórico) tem **~2 790 tokens**, e o engine hoje limpa o KV
cache a cada chamada (ADR-003) → **~6,4 min de prefill por turno**, inclusive em
heartbeat (a cada 30 min) e cron. Em CPU sem AVX-512-VNNI, o prefill do `Q1_0` é tão
lento quanto a geração (kernel repack só existe pra AVX512-VNNI; AVX2 cai em
`vec_dot` token-a-token) — reduzir e reaproveitar tokens é a única alavanca de ganho
grande disponível.

O PicoClaw já tem um mecanismo de perfil de turno (`pkg/config/turn_profile.go` →
`EffectiveTurnProfile` → `filterToolsByTurnProfile`, `pkg/agent/pipeline_llm.go:39-40`),
mas ele é **estático e global** — não varia por mensagem, não reduz o custo de
heartbeat/cron. A direção dada pelo André: modularidade; o prompt inicial funcionando
como um *router*; "janelas de foco" e "gatilhos de atenção".

## Decisão
1. **Roteador determinístico por mensagem, 0 tokens de LLM**
   (`pkg/routing/focus.go`, sem importar `pkg/config`, no mesmo padrão do
   `RouterConfig` já existente em `pkg/routing/router.go`): decide a janela por
   precedência — janela explícita → tag inline `[foco:files] …` na mensagem →
   `Origins` (heartbeat/cron/system, não-usuário) → regras da config na ordem
   (keyword com fold de acento, depois regex) → `MicroRouter` opcional (gancho para um
   roteador LLM futuro; `nil` por padrão, não implementado agora) → aderência de
   sessão (`Remember/Forget/Current`) → `Default`. Custo: microssegundos, 0 tokens.
2. **Cada janela restringe tools/skills/histórico/memória/modo de prompt**
   (`pkg/config/focus.go`, `FocusConfig`/`FocusWindow`). Janela padrão `chat` roda
   **sem tools**. Janelas sensíveis a hora (`schedule`, heartbeat, cron) recebem
   `NeedsTime=true`.
3. **Prompt-núcleo compacto**: identidade reduzida a ≤~150 tokens de overhead de
   framework; contexto dinâmico por padrão restrito a `## Current Date` (sem carimbo
   de minuto no meio do system prompt) — o prefixo (identidade + workspace + memória +
   tools + histórico antigo) fica **byte-idêntico o dia inteiro**, o que é o que torna
   o cache de prefixo (ADR-015) eficaz. Quando a janela precisa de hora
   (`NeedsTime`), o carimbo `[now: …]` vai no **fim da mensagem de usuário montada**
   (nunca persistido, nunca no meio do system).
4. **Schema de tools compacto**: descrição na primeira frase (≤100 chars), parâmetros
   reduzidos a `type` (+ `enum`≤8, `items:{type}`) — sem remover propriedades (uma tool
   como `exec` só declara `action` como `required`; "só required" perderia `command`).
5. **Escalonamento bounded (1×/turno)**: quando o modelo pede uma tool fora da janela
   ativa, a verificação segue uma ordem estrita:
   1. **Registro global primeiro** — nome inexistente ou tool desabilitada na config
      é **rejeição imediata**, sem escalar e sem tocar o KV, com dica de nome parecido
      (distância de edição sobre as tools disponíveis na janela atual).
   2. Só se a tool **existe no catálogo global mas está fora da janela** é que o
      escalonamento acontece: sobe para `EscalateTo` (padrão a janela `full`), no
      máximo 1× por turno.
   3. `denyByTurnProfile` continua como guarda final.
6. **Reflexos** (`pkg/agent/reflex.go`): caminho de **0 tokens antes do roteador** —
   regex determinístico → `reply` (template), `command` (slash command existente) ou
   `exec` (mesmo gating de `pkg/tools` — deny patterns, restrict, timeouts). Cobre
   turnos triviais (saudação, "status", "uptime") em <1s.
7. **Heartbeat e cron ganham janelas próprias**, curtas e sem histórico
   (`Origin: "heartbeat"|"cron"`), reduzindo o custo do que hoje paga o prompt inteiro
   a cada disparo periódico.

> **Ajuste incorporado (comentário do André)**: a verificação do passo 5.1 (registro
> global antes de escalar) não é uma ideia nova sendo adicionada — é a explicitação de
> um comportamento que já estava implícito ("tool registrada mas fora da janela"). O
> ganho concreto: sem essa checagem, um nome de tool **alucinado** (o Bonsai 1.7B erra
> sintaxe com frequência) forçaria uma escalada de janela inteira — e mesmo com
> ADR-015 evitando o re-prefill completo, ainda seria trabalho desperdiçado para no
> final descobrir que a tool não existe em lugar nenhum. Com a checagem, a rejeição é
> imediata e o erro devolvido ao modelo já inclui a dica de correção.

## Alternativas
- **Manter o `turn_profile` estático/global**: rejeitada como única solução — não
  varia por mensagem nem reduz o custo do heartbeat/cron; continua existindo como o
  mecanismo de baixo nível que as janelas configuram (`EffectiveTurnProfile` ganha
  campos novos: `Window`, `MemoryMode`, `NeedsTime`, `EscalateTo`).
- **Roteador via segunda chamada ao LLM** (classificar a mensagem antes de responder):
  rejeitada por ora — custaria tokens e uma segunda rodada de latência num modelo já
  lento. Fica como gancho (`MicroRouter`, interface, `nil` por padrão) para um
  micro-roteador futuro, sem comprometer esta entrega.

## Consequências
- Janela `chat` sem tools; heartbeat ~600 tokens (era ~2 790); reflexos <1s a 0 tokens.
- Mais uma camada de configuração (janelas, triggers, escalonamento) — risco de "tool
  sumiu" numa janela errada; mitigado pela rejeição imediata de tool inexistente + o
  hint de nome parecido + `denyByTurnProfile` como guarda final.
- Com `focus.enabled=false` e sem `reflexes` configurados, o comportamento é
  **idêntico** ao `turn_profile` estático de hoje — bytes do system prompt idênticos,
  schemas verbatim, sem escalonamento (ver seção de compatibilidade do plano-fonte).
- Telemetria (ADR-017) passa a contar `escalations` e `unknown_tool_calls` por turno —
  dado real para calibrar janelas e triggers depois.
