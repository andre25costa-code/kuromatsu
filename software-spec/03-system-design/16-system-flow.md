---
id: S16
title: Fluxo do sistema — reflexo → roteador de foco → janela → escalonamento
status: confirmed
version: 2
owner: André
last_updated: 2026-09-12
depends_on: [S11, S13, S18]
---

# S16 — Fluxo do sistema

> **Status**: `confirmed` — ADR-014 (`accepted`), plano `demetrius`. Este item estava
> `missing` (o fluxo interno herdado do PicoClaw era derivável do código sem
> documentação própria). Com o roteador de foco, reflexos e escalonamento, o fluxo
> mudou o suficiente para merecer registro explícito, separado do fluxo de negócio
> (S07, `n/a` neste projeto) e da arquitetura de componentes (S13).

## Fluxo de uma mensagem de usuário

```mermaid
flowchart TD
    MSG[Mensagem recebida<br/>Telegram/WhatsApp/pico/CLI] --> CMD{É slash command?}
    CMD -->|sim| HC[handleCommand]
    CMD -->|não| RX{Reflexo casa?<br/>regex, 0 tokens}
    RX -->|sim| RXACT[reply / command / exec<br/>0 tokens, herda gating de pkg/tools]
    RX -->|não| RT[Roteador de foco<br/>pkg/routing/focus.go — 0 tokens]
    RT --> WIN[Janela resolvida<br/>tools+skills+histórico+memória<br/>+ prompt-núcleo compacto]
    WIN --> RS{{runstate:<br/>Inc(Inference)}}
    RS --> PIPE[Pipeline LLM<br/>fallback chain]
    PIPE --> ENG[Engine nativo<br/>cache de prefixo do KV]
    ENG --> OUT{Saída pede tool<br/>fora da janela?}
    OUT -->|tool não existe no catálogo global| REJ[Rejeição imediata<br/>com dica de nome parecido<br/>sem escalar, sem tocar o KV]
    OUT -->|tool existe, fora da janela,<br/>escalations < 1| ESC[Escala para EscalateTo<br/>reconstrói só o system<br/>focusEscalations++]
    ESC --> PIPE
    OUT -->|tool dentro da janela<br/>ou sem mais tool call| RESP[Resposta final ao usuário]
    REJ --> PIPE
    RXACT --> DONE[Turno registrado<br/>telemetria]
    RESP --> DONE
```

## Passo a passo

1. **Comando**: se a mensagem é um slash command (`/foco`, `/use`, `/context`…),
   segue o caminho existente de `handleCommand` — reflexos e roteador não entram.
2. **Reflexo** (FR-013): regex determinístico, checado **antes** do roteador, só para
   origem `user`. Casando, o turno termina em <1s a 0 tokens — nem o roteador nem o
   pipeline LLM são acionados.
3. **Roteador de foco** (FR-014): sem reflexo, a mensagem é roteada por precedência
   (explícito → tag inline → origem não-usuário → regras keyword/regex → aderência de
   sessão → `Default`) para uma janela. A janela determina o subconjunto de
   tools/skills/histórico/memória e o modo de prompt (compact/core, ver S21 para o
   cache do núcleo).
4. **`runstate.Inc(Inference)`** (S09) marca o início da ocupação — heartbeat/cron
   concorrentes pulam (S30); memguard pode negar o load sob pressão de memória (S17).
5. **Pipeline LLM**: a chamada ao engine se beneficia do cache de prefixo (S21) — só o
   delta desde a última chamada na mesma janela é reprocessado.
6. **Tool call fora da janela**: verificação em duas etapas (ADR-014) —
   1. **Registro global primeiro**: nome inexistente/tool desabilitada →
      rejeição imediata, sem escalar, com dica de nome parecido por distância de
      edição. O modelo tenta de novo pagando só o delta (não o prompt inteiro).
   2. Só se a tool existe globalmente mas está fora da janela → escalonamento (no
      máximo 1×/turno) para `EscalateTo`; a resposta rejeitada não é persistida.
7. **Origem não-usuário** (heartbeat/cron): mesmo fluxo, mas a janela vem de
   `Origins` (não das regras de keyword) e tipicamente roda sem histórico
   (`NoHistory`) — ver S30 para o desenho completo de agendamento.

## Diferença de S07 (fluxo de negócio)

S07 está `n/a` neste projeto (não há fluxo de negócio externo ao repo — as rotinas
pessoais do usuário vivem fora do repo público). Este capítulo (S16) é **puramente
interno**: como o software decide o que processar e com quanto contexto, não como o
usuário usa o produto no seu dia a dia.

## Cross-referências

- **Tabela de janelas padrão** (tools/skills/memória/histórico/aderência/triggers por
  janela): ADR-014, config em `pkg/config/focus.go`.
- **Cache de prefixo e núcleos de janela**: S21.
- **Estados do `runstate` e regras de preempção**: S09.
- **Retry/degradação quando o escalonamento ou o load do modelo falham**: S17.
