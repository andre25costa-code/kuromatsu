---
id: S13
title: Arquitetura geral
status: confirmed
version: 1
owner: André
last_updated: 2026-09-10
depends_on: [S10, S18]
---

# S13 — Arquitetura geral

Um único binário Go (`kuromatsu`). A inferência local é código C/C++ do fork prism
linkado **estaticamente dentro do binário** (cgo, build tag `nativellm`); não há
processo nem porta de modelo.

```mermaid
flowchart TB
    TG[Telegram] --> GW
    WA[WhatsApp] --> GW
    PC[canal pico / CLI] --> GW
    subgraph bin [binário kuromatsu]
        GW[gateway] --> AL[agent loop]
        CRON[cron service] --> AL
        HB[heartbeat] --> AL
        SLEEP[modo dormir<br/>pkg/sleep] -->|ChatFunc| PIPE
        AL --> PIPE[pipeline LLM<br/>fallback chain]
        PIPE -->|chaves API| EXT[providers HTTP<br/>openai / anthropic / ...]
        PIPE -->|fallback nativo| LLLM[pkg/providers/localllm<br/>cgo estático]
        AL --- TOOLS[tools: fs, exec, cron,<br/>message, sysmon, skills]
        AL --- MEM[(sessões JSONL +<br/>seahorse SQLite FTS5)]
    end
    LLLM --> GGUF[(Bonsai-1.7B-Q1_0.gguf<br/>mmap, volume /models)]
    EXT -.-> NET((internet))
```

## Relações-chave

| De → Para | Contrato |
|---|---|
| pipeline → providers | interface `LLMProvider.Chat(messages, tools, model, options)`; candidatos resolvidos pela fallback chain existente |
| factory → localllm | `case "native"` em `CreateProviderFromConfig`; opções vindas de `ModelConfig.ExtraBody` |
| localllm → llama.cpp | funções C de `include/llama.h`; libs `.a` produzidas pelo cmake do submodule |
| sleep → pipeline | `ChatFunc` construída sobre `resolveModelCandidates` + `ExecuteCandidate` (o "inconsciente" é qualquer modelo da model_list) |
| sleep ← evolution | reusa `evolution.ColdPathRunner` (single-flight + coalescing) |
| config → chain | `ApplyNativeFallback` injeta `bonsai-local` (padrão sem chaves; último fallback com chaves) |

## Sistemas externos

- **Provedores LLM via HTTP** — opcionais, só com chave em `config.json`/`.security.yml`.
- **Telegram/WhatsApp** — canais de entrada/saída.
- **GitHub (PrismML-Eng/llama.cpp)** — origem do submodule pinado.
- **Fonte do GGUF** (S25, TBD) — usada apenas pelo script de download; runtime nunca baixa nada.
