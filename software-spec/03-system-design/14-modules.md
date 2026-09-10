---
id: S14
title: Módulos do sistema
status: confirmed
version: 1
owner: André
last_updated: 2026-09-10
depends_on: [S13]
---

# S14 — Módulos do sistema

Só os módulos tocados por esta refatoração; o restante do PicoClaw permanece como está.

| Módulo | Situação | Responsabilidade | Fronteira |
|---|---|---|---|
| `pkg/providers/localllm` | **novo** | Binding cgo do llama.h; render/parse ChatML; adapter `LLMProvider`; single-flight; keep-alive | Importa só `protocoltypes` (sem ciclo com `pkg/providers`); C limitado a `engine_cgo.go` |
| `pkg/providers` (factory/catálogo) | estendido | Registrar provider `native` (aliases `bonsai`, `kuro`) e criar via `ExtraBody` | Nenhuma mudança nos providers HTTP existentes |
| `pkg/config` | estendido | Entrada semeada `bonsai-local`; `ApplyNativeFallback`; bloco `sleep`; shim de env legado | Não conhece llama.h; checa GGUF por caminho |
| `pkg/sleep` | **novo** | Janela noturna: coleta → triagem LLM → aplicação (MEMORY.md, compactação seahorse, relatório) | Recebe `ChatFunc` injetada; nunca chama provider direto |
| `pkg/agent/sleep_bridge.go` | **novo** | Timer da janela; monta `ChatFunc` sobre a chain; respeita BR-001 | Espelha `evolution_bridge.go`; reusa `ColdPathRunner` |
| `pkg/tools` | estendido | `sysmon` (mem/top/load/disk/proc) substitui `hardware/` | Ações destrutivas gated (BR-007) |
| `pkg/channels` | **podado** | Ficam `pico`, `pico_client`, `telegram`, `whatsapp`, `whatsapp_native` | Registro por factory permanece |
| `web/` | **removido** | — | — |
| `workspace/` | substituído | Template genérico PT-BR + skill `kuromatsu-docs`; embed do onboard | Conteúdo pessoal só em `docker/data/` |
| `Makefile` / `docker/` | estendido | Targets `llama-lib*`, `build-native*`, `model-download`; `Dockerfile.native`; compose 1 GB | `make build` padrão intocado (BR-004) |
| `cmd/nativebench` | **novo** | Medir tok/s + RSS do engine nativo (baseline S39) | Usa `localllm` direto, sem agente |
