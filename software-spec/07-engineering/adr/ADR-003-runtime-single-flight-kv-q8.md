---
id: ADR-003
title: Runtime do modelo — single-flight, KV q8_0, ctx 2048, keep-alive com unload
status: superseded
superseded_by: ADR-015
version: 2
owner: André
last_updated: 2026-09-11
depends_on: [ADR-001]
---

# ADR-003 — Runtime: single-flight, KV q8_0, ctx 2048, keep-alive

> **Superseded por [ADR-015](ADR-015-runtime-v2-cache-prefixo-kv.md) em 2026-09-11**
> (`proposed`, aguardando confirmação do André). As decisões 1 e 2 abaixo
> (single-flight; KV `q8_0` como default) **continuam valendo sem mudança** — a
> ADR-015 as reafirma. O que muda: a v1 aqui limpa a memória KV inteira a cada
> chamada (decisão 3, "recarga sob demanda", implicava reprocessar o prompt completo
> todo turno); a ADR-015 introduz cache de prefixo entre chamadas + núcleos de janela
> em RAM, keep-alive contado do fim da chamada (`-1` = nunca), `abort_callback`
> interrompendo o prefill, e uma guarda de reload por `loadKey` que não recarrega o
> modelo a cada `max_tokens`/`temperature` diferente. Mantida como registro histórico
> da decisão original — não implementar contra esta versão.

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
