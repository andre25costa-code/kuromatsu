---
id: E6
title: Fallback nativo automático e diagnóstico
status: done
frs: [FR-003, FR-004]
adrs: [ADR-002]
commits: [e8c1c097]
last_updated: 2026-09-11
---

# E6 — Fallback nativo automático

## O que foi entregue

O provider `native` (aliases `bonsai`, `kuro`) entra no catálogo/factory/
fallback-chain existentes do PicoClaw como um candidato de modelo comum
(ADR-002):

- `pkg/providers/provider_metadata.go`: entrada no catálogo (`Local`,
  `EmptyAPIKeyAllowed`, sem `CommonModels` — mesmo invariante já usado por
  outros providers locais).
- `pkg/providers/factory_provider.go` + `native_options.go`: `case "native"`
  resolve o caminho do GGUF via `localllm.ResolveModelPath` e mapeia
  `ExtraBody` (`n_ctx`, `kv_cache_type`, `n_threads`, `n_batch`,
  `keep_alive_secs`, `max_predict`) para `localllm.Options`.
- `pkg/config/defaults.go`: entrada semeada `bonsai-local`
  (`native/Bonsai-1.7B-Q1_0`, `n_ctx: 2048`, `kv_cache_type: q8_0`).
- `pkg/config/native_fallback.go`: `ApplyNativeFallback`, chamada nos 3
  caminhos de `LoadConfig` que produzem um config (arquivo carregado,
  ausente, quase vazio). No-op a menos que o binário tenha o engine
  `nativellm` **e** o GGUF esteja em disco (BR-002): sem chave configurada em
  nenhum provider e sem default explícito, `bonsai-local` vira
  `agents.defaults.model_name`; senão é anexado uma vez a `model_fallbacks`.
  Seam `nativeBuilt` (default `localllm.Built`) permite testar sem build cgo
  real.
- Diagnóstico: linha "Native (Bonsai)" em `kuromatsu status`
  (`cmd/kuromatsu/internal/status`); `kuromatsu model bonsai-local` recusa com
  dica de rebuild em binário sem o engine, em vez de configurar um modelo
  morto.

## FRs cobertas

| FR | Descrição | ACs | Status |
|---|---|---|---|
| FR-003 | Fallback nativo automático | AC-003-1, AC-003-2, AC-003-3 | Verificado |
| FR-004 | Diagnóstico do provider nativo | AC-004-1, AC-004-2 | Verificado |

8 testes novos em `pkg/config/native_fallback_test.go` cobrindo: não
buildado, GGUF ausente, entrada removida pelo usuário, torna-se default,
anexado como fallback, respeita default explícito, idempotente.

## Commits

| Commit | Mensagem | Diff |
|---|---|---|
| `e8c1c097` | feat(providers): wire the native provider into the fallback chain (E6) | 9 arquivos, +357/-2 |

**Nota de caminho**: no momento deste commit o diretório de comandos ainda era
`cmd/picoclaw/internal/{status,model}` — o rename para `cmd/kuromatsu/...`
só aconteceu depois, no E3 (posterior na sequência de entregáveis, embora
E6 tenha sido commitado antes de E3 na timeline real do git). Hoje os
arquivos vivem em `cmd/kuromatsu/internal/status/` e não há mais
`cmd/picoclaw/` no repo — confirmado nesta auditoria.

## Arquivos-chave

`pkg/providers/provider_metadata.go`, `pkg/providers/factory_provider.go`,
`pkg/providers/native_options.go`, `pkg/config/defaults.go`,
`pkg/config/native_fallback.go` + `native_fallback_test.go`,
`cmd/kuromatsu/internal/status/`, `cmd/kuromatsu/internal/model/`.

## Como verificar

`git show --stat e8c1c097`; `go test ./pkg/config/... -run NativeFallback`.

## Notas

Nenhum gap conhecido para este entregável.
