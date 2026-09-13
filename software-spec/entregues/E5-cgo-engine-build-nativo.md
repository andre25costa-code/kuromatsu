---
id: E5
title: Engine cgo real e targets de build nativo
status: done
frs: [FR-001, FR-005]
adrs: [ADR-001, ADR-003]
commits: [285c8843]
last_updated: 2026-09-11
---

# E5 — Engine cgo e build nativo

## O que foi entregue

`pkg/providers/localllm/engine_cgo.go` (build tag `nativellm && cgo`), o
binding real com o llama.cpp do fork prism, fechando o desenho do E4:

- Lazy load do modelo; um único `llama_context` por processo, guardado por
  mutex (ADR-003, single-flight); tipo de KV cache selecionável por
  `Options.KVCacheType` (default `q8_0`).
- Timer de keep-alive que descarrega o modelo após idle.
- Detecção de context-overflow antes do decode (margem fixa) **e** a cada
  passo de decode via `llama_memory_seq_pos_max` (mesmo padrão de contabilidade
  do `examples/simple-chat` do próprio llama.cpp).
- Cancelamento por `ctx.Done()` durante a geração.
- v1 **sem** reuso de KV entre chamadas: `llama_memory_clear` no início de todo
  `completion()` — cada turno reprocessa o prompt completo. Decisão deliberada
  de simplicidade para o workload assíncrono do S01 (cache de prefixo é
  trabalho posterior, ver Trilho B do plano de refatoração aprovado).
- `Makefile`: `llama-lib`/`llama-lib-arm64` (build estático via cmake do
  submodule pinado), `build-native`/`build-native-arm64` (`CGO_ENABLED=1 go
  build -tags nativellm`), `run-native`, `bench-native`.
- `cmd/nativebench`: ferramenta standalone (roda 3 prompts fixos, mede tok/s e
  RSS via `/proc/self/status`) — é o que produziria a baseline real do S39 uma
  vez rodada na máquina alvo.

## FRs cobertas

| FR | Descrição | ACs | Status |
|---|---|---|---|
| FR-001 | Inferência nativa in-process | AC-001-1..5 | Verificado (ver nota de validação) |
| FR-005 | Build nativo reprodutível | AC-005-1, AC-005-3 | Verificado neste entregável; AC-005-2/4/5/6 fecham no E7 |

**Validação real**: `TestCgoEngine_RealModel_Integration` carregou o GGUF real
em WSL/x86_64 (máquina de dev — hoje sabidamente a mesma arquitetura do alvo
real, ver ADR-012, embora não a mesma CPU exata) e obteve uma resposta
coerente ("Olá!") a um prompt em português;
`TestCgoEngine_ContextOverflow_Integration` confirmou o caminho de overflow.
Buffers reais medidos e registrados em S18/S29: modelo (CPU_Mapped) 231,13
MiB; KV q8_0 @ n_ctx=512 29,75 MiB; buffer de computação 152,12 MiB — maior
que a estimativa anterior de ~60 MiB, que precisou ser revista.

## Commits

| Commit | Mensagem | Diff |
|---|---|---|
| `285c8843` | feat(localllm): add the cgo engine and native build targets (E5) | 8 arquivos, +561/-8 |

## Arquivos-chave

`pkg/providers/localllm/engine_cgo.go`, `pkg/providers/localllm/engine_cgo_test.go`,
`Makefile` (targets `llama-lib`, `llama-lib-arm64`, `build-native`,
`build-native-arm64`, `run-native`, `bench-native`), `cmd/nativebench/{main,rss_linux,rss_other}.go`.

## Como verificar

`git show --stat 285c8843`; `grep -n "llama-lib\|build-native\|nativebench" Makefile`;
teste de integração real requer `KUROMATSU_INTEGRATION_TESTS=1 KUROMATSU_TEST_MODEL=<path>`.

## Notas / gap conhecido — resolvido no E7

O próprio commit registrou um gap: o binário completo `kuromatsu` com `-tags
nativellm` **não** linkou com sucesso na máquina de dev (4 tentativas mortas
por um guarda de memória baixa do host durante o link, com paralelismo
reduzido até `-j 1`) — isolado como teto de recurso do host de dev (~8 GB
físicos), não um defeito de código, já que o pacote `localllm` isoladamente
compila/linka/roda correto. O próprio commit apontou o build Docker do E7
como o próximo teste real disso. **Confirmado resolvido**: o E7 buildou e
rodou a imagem completa com sucesso no runner do GitHub Actions (16 GB de
RAM) e, mais adiante, na própria Oracle via `docker load` — ver
`entregues/E7-deploy-nativo-oracle.md`.
