---
id: ADR-002
title: Provider "native" no catálogo existente, ref native/Bonsai-1.7B-Q1_0
status: accepted
version: 1
owner: André
last_updated: 2026-09-10
depends_on: [ADR-001]
---

# ADR-002 — Provider `native` no catálogo de providers

## Contexto
O PicoClaw resolve modelos por um catálogo (`provider_metadata.go`) + factory
(`CreateProviderFromConfig`) + fallback chain. A inferência embutida precisa aparecer
como um provider comum para herdar candidatos, cooldown, retry e configuração.

## Decisão
Registrar o provider **`native`** (aliases `bonsai`, `kuro`) com `Local: true`,
`EmptyAPIKeyAllowed: true`, `CreateAllowed`, `DefaultModelAllowed`. Ref de modelo:
`native/Bonsai-1.7B-Q1_0`. Entrada semeada `bonsai-local` no `defaults.go` com
`ExtraBody: {n_ctx: 2048, kv_cache_type: "q8_0"}` — knobs por modelo viajam no
`ExtraBody` já existente, sem bloco de config novo.

## Alternativas
- Provider chamado `bonsai` (nome do modelo, não do transporte) — vira alias.
- Reaproveitar o alias `local-model` (aponta VLLM) — não retargetear para não quebrar
  usuários; apenas mencionar `bonsai-local` na ajuda.

## Consequências
- Nenhuma mudança nos providers HTTP; um `case` novo na factory + uma entrada no catálogo.
- O caminho do GGUF resolve por: absoluto → `$KUROMATSU_MODELS_DIR` → `<home>/models/` → `./models/`.
