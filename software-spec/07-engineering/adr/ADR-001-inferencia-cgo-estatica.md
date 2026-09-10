---
id: ADR-001
title: Inferência in-process via cgo + bibliotecas estáticas, atrás de build tag
status: accepted
version: 1
owner: André
last_updated: 2026-09-10
depends_on: []
---

# ADR-001 — Inferência in-process via cgo + libs estáticas (build tag `nativellm`)

## Contexto
O Kuromatsu deve operar o Bonsai-1.7B-Q1_0 "nativamente": sem compilar llama.cpp à
parte em produção, sem llama-server, sem expor o modelo na rede. O PicoClaw é 100%
`CGO_ENABLED=0` e cross-compila para uma matriz grande de plataformas — isso não pode
quebrar (C4/BR-004).

## Decisão
Compilar `libllama`/`libggml` do fork prism como bibliotecas estáticas (`.a`) via CMake
mínimo (CPU-only, sem server/examples/common) e linkar no binário Go com **cgo**,
protegido pela build tag **`nativellm`**. Sem a tag, um stub Go puro devolve
`ErrNotBuilt` — mesmo padrão do systray (`web/backend/systray_stub_nocgo.go`).

## Alternativas consideradas
- **Subprocesso embutido** (llama-cli via stdin/stdout): evita cgo, mas 2 processos,
  protocolo frágil, mais RAM (dois runtimes) — rejeitada.
- **purego + libllama.so**: evita toolchain C no build Go, mas exige distribuir o `.so`
  junto — deixa de ser "um binário só" — rejeitada.

## Consequências
- Um único binário auto-suficiente; inferência é uma chamada de função.
- Builds nativos exigem toolchain C++ → resolvido com Docker multi-stage (ADR/S32).
- O default `make build` permanece Go puro em toda a matriz (NFR-003).
