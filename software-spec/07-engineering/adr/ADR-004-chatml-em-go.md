---
id: ADR-004
title: Template ChatML/Qwen3 renderizado e parseado em Go, não em C++
status: accepted
version: 1
owner: André
last_updated: 2026-09-10
depends_on: [ADR-001]
---

# ADR-004 — ChatML/Qwen3 em Go puro

## Contexto
O tool-calling do Bonsai usa o template ChatML/Qwen3 gravado no GGUF (`# Tools`,
`<tools>`, `<tool_call>`). No llama.cpp, `llama_chat_apply_template` **não tem
parâmetro de tools** e não usa jinja; quem suporta tools é `common/chat.cpp` — C++
sem ABI C, que arrastaria `common/jinja`, nlohmann, parsers PEG etc.

## Decisão
Renderizar o prompt e parsear a saída **em Go** (`pkg/providers/localllm/chatml.go`):
template fixo conhecido (o do GGUF), parser tolerante (`<think>` → reasoning;
`<tool_call>` JSON → calls; malformado → `{"raw": ...}`; máx. 4 calls). `LLAMA_BUILD_COMMON=OFF`.

## Alternativas
- Linkar `common/chat.cpp` com shim `extern "C"` — grande superfície C++ instável para
  um único template — rejeitada.

## Consequências
- Testável no Windows sem cgo (golden tests, FR-002); menos código C na fronteira.
- Se um futuro modelo usar outro template, será outro renderer Go (decisão por modelo).
