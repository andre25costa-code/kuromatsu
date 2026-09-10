---
id: ADR-003
title: Runtime do modelo — single-flight, KV q8_0, ctx 2048, keep-alive com unload
status: accepted
version: 1
owner: André
last_updated: 2026-09-10
depends_on: [ADR-001]
---

# ADR-003 — Runtime: single-flight, KV q8_0, ctx 2048, keep-alive

## Contexto
1 GB de RAM total (C1). O KV cache do qwen3-1.7B custa ~112 KB/token em f16; o modelo
mapeado ocupa 237 MB; o agente Go precisa de 200–300 MB.

## Decisão
1. **Um único `llama_context` por processo**, protegido por mutex — chamadas concorrentes
   serializam (AC-001-4). Registry por model-path garante uma instância mesmo com a
   factory sendo chamada por agente/candidato.
2. **KV cache `q8_0`** por padrão (≈ 59 KB/token) e **`n_ctx=2048`** operacional
   (~118 MB de KV), configuráveis por `ExtraBody`.
3. **Lazy load + keep-alive**: modelo carrega na primeira chamada; após
   `keep_alive_secs` (default 300) sem uso, `unload()` libera contexto+modelo
   (NFR-004). Recarga sob demanda.
4. **Overflow pré-checado**: prompt + margem 64 > n_ctx → erro com texto
   `context_length_exceeded`, que o `ClassifyError` existente mapeia para
   `FailoverContextOverflow` (não-retriable) sem import de `pkg/providers`.

## Consequências
- Pico estimado 650–720 MB (S29); heartbeat/cron/sono disputam o mesmo contexto — sono
  só roda sem turn ativo (BR-001).
- Latência de primeira resposta após idle inclui o reload do modelo (aceitável: workload
  assíncrono).
