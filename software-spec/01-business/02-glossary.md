---
id: S02
title: Glossário
status: confirmed
version: 1
owner: André
last_updated: 2026-09-10
depends_on: []
---

# S02 — Glossário

| Termo | Definição | Onde aparece |
|---|---|---|
| **Kuromatsu** | Este projeto: fork do PicoClaw com inferência 1-bit embutida. Binário `kuromatsu`, home `~/.kuromatsu`, env `KUROMATSU_*` | todo o repo |
| **PicoClaw** | Projeto upstream (`github.com/sipeed/picoclaw`), agente Go ultraleve | histórico git |
| **Bonsai** | Família de modelos da PrismML treinados quantizados em 1 bit | `models/` |
| **Fork prism** | `PrismML-Eng/llama.cpp`, branch `prism` — o runtime C/C++ que implementa o tipo `Q1_0`. Não é uma biblioteca separada chamada "prism" | submodule `llama.cpp/` |
| **Q1_0** | Tipo de quantização 1-bit real (1.125 bpw): 1 bit de sinal por peso + escala fp16 por bloco de 128. `GGML_TYPE` 41, `LLAMA_FTYPE` 40 | GGUF, kernels ggml |
| **Provider nativo** | Provider `native` do catálogo: inferência in-process via cgo, sem rede. Aliases: `bonsai`, `kuro` | `pkg/providers/localllm` |
| **Fallback chain** | Cadeia de candidatos de modelo do PicoClaw (`pkg/providers/fallback.go`); em caso de falha, tenta o próximo | config `model_fallbacks` |
| **Build tag `nativellm`** | Tag de build que liga o engine cgo; sem ela o build é 100% Go puro e o provider nativo responde `ErrNotBuilt` | Makefile, `engine_*.go` |
| **Modo dormir** | Rodada noturna opcional de consolidação de memória (poda + reforço) executada pelo agente fora do horário de uso | `pkg/sleep`, config `sleep` |
| **Inconsciente** | O modelo LLM usado pela triagem do modo dormir — qualquer ref da `model_list` (API externa ou o próprio Bonsai local) | config `sleep.unconscious_model` |
| **Workspace** | Diretório de personalidade/estado do agente (`AGENT.md`, `SOUL.md`, `USER.md`, `HEARTBEAT.md`, skills, sessões). O repo versiona apenas um **template** | `workspace/` |
| **Hub pessoal** | O workspace privado do André (hub de estudos com `hub_core` em Python), que vive em `docker/data/workspace/` — gitignorado, nunca publicado | fora do repo |
| **Seahorse** | Gerenciador de contexto opcional do PicoClaw: SQLite + FTS5 (BM25), Go puro | `pkg/seahorse` |
| **GGUF** | Formato de arquivo de modelo do ecossistema llama.cpp | `models/*.gguf` |
