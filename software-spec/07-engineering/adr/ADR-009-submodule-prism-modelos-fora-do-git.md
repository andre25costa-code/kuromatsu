---
id: ADR-009
title: llama.cpp como submodule pinado; GGUFs fora do git com download verificado
status: accepted
version: 2
owner: André
last_updated: 2026-09-11
depends_on: [ADR-001]
---

# ADR-009 — Submodule pinado + modelos fora do git

## Contexto
O tipo `Q1_0` só existe no fork prism (C3) — o build precisa de uma versão exata dessa
árvore. O GGUF tem 237 MB e o GitHub rejeita arquivos > 100 MB (C2). Hoje `llama.cpp/`
é um clone solto untracked e `models/` não está no `.gitignore`.

## Decisão
1. Registrar `llama.cpp/` como **git submodule** de `PrismML-Eng/llama.cpp`, pinado em
   `d8f26eec7` (o clone existente é aproveitado in-place). Atualização de pin é sempre
   um commit deliberado.
2. `models/*.gguf` no `.gitignore`; obtenção via `scripts/download-model.sh`
   (+`make model-download`) com verificação SHA256 e idempotência (FR-012). Fonte
   oficial: confirmada em S25 (`huggingface.co/prism-ml/Bonsai-1.7B-gguf`; ver S40).
3. Artefatos de build C (`llama.cpp/build-native/`) também ignorados.

## Alternativas
- Vendorizar a árvore C no repo: 100+ MB de fonte, perde rastreio do upstream — rejeitado.
- Só documentar "clone o fork": builds não reprodutíveis — rejeitado.

## Consequências
- `git clone --recursive` passa a ser necessário para builds nativos (documentar na skill).
- CI/Docker validam o pin: estágio de build falha se o SHA divergir.
