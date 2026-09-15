---
id: ADR-015
title: Runtime do modelo v2 — cache de prefixo do KV, núcleos de janela em RAM, keep-alive/abort/threads revisados
status: accepted
version: 1
owner: André
last_updated: 2026-09-12
depends_on: [ADR-001, ADR-003]
---

# ADR-015 — Runtime v2: cache de prefixo do KV entre chamadas

## Contexto
A ADR-003 decidiu single-flight, KV `q8_0`, `n_ctx=2048` operacional e keep-alive com
unload — mas a v1 **limpa a memória KV e reprocessa o prompt inteiro em toda
chamada**. Com o prompt-base em ~2 790 tokens (ADR-014) e sem o kernel GEMM repack
otimizado do `Q1_0` disponível em CPUs sem AVX-512-VNNI (nem o Zen1 da Oracle, nem os
hosts observados na `demetrius` — Xeon/EPYC 7B12, ambos só AVX2), o prefill cai para a
mesma velocidade da geração token-a-token: **~6,4 min por turno**, mesmo em heartbeat.
A API do fork prism suporta `llama_memory_seq_rm` parcial e
`llama_state_seq_save_file/load_file` — a base técnica para reaproveitar trabalho já
existe, só não estava sendo usada.

## Decisão
1. **Cache de prefixo do KV entre chamadas** (`engine_cgo.go`, novo `prefix.go`): o
   engine mantém `kvTokens []C.llama_token` (o que de fato está decodificado no KV).
   A cada `completion`, tokeniza o prompt inteiro, calcula
   `nCommon = commonPrefixLen(kvTokens, tokens)`, remove do KV o que diverge
   (`llama_memory_seq_rm(mem, 0, nCommon, -1)`) e decodifica só o delta
   (`llama_batch_get_one(&tokens[nCommon], …)`). **Invariante**: `kvTokens` reflete
   exatamente os tokens de decodes bem-sucedidos; erro/abort → `llama_memory_clear` +
   `kvTokens = nil` (nunca um estado parcialmente inconsistente).
2. **Núcleos das janelas guardados na RAM, dentro do próprio KV cache** — sem disco no
   caminho quente: `kv_unified=true`, `n_seq_max = 1+N` (N janelas cacheadas, padrão
   2). A conversa ativa vive na sequência 0; ao final do prefill do núcleo de uma
   janela `k`, `llama_memory_seq_cp(mem, 0, k, 0, nCore)` marca as células como
   pertencentes também à sequência `k` — **zero cópia de dados** (mesma técnica que o
   `llama-server` usa para compartilhar prefixos entre slots). Troca de janela:
   `seq_rm` do que diverge + `seq_cp` restaura o núcleo em milissegundos.
3. **Persistência em disco só no restart do processo**, opcional
   (`ExtraBody.kv_state_dir`, `"off"` desliga): `llama_state_seq_save_file` de cada
   núcleo quando o `runstate` está `Idle`; no boot, `load_file` das janelas mais
   usadas antes do serviço ficar `ready`. Gate de aceite: `load_ms` ≤5s e sem pico de
   `iowait` >30% no boot — medido com `dd` sequencial na `demetrius`
   (pd-standard: 74-137 MB/s, iowait 0), então viável, mas o mecanismo em RAM (item 2)
   já resolve o caminho quente sem depender dessa medição.
4. **Keep-alive contado do FIM da chamada** (não do início como hoje); `-1` explícito
   = nunca descarregar (era ambíguo com `0`, que continua significando 300s default).
5. **`abort_callback`**: `llama_set_abort_callback` ligado a um `atomic.Bool` armado
   por `context.AfterFunc(ctx, …)` a cada `completion` — cancelamento interrompe o
   **prefill**, não só a geração (hoje um cancelamento no meio de um prefill de
   ~1 500 tokens não tinha efeito prático).
6. **`n_threads = min(runtime.NumCPU(), 4)`**: a `demetrius` tem 1 core físico/2
   threads (HT) — usar até 4 sem exceder o hardware disponível, configurável.
7. **Guarda de reload por `loadKey`**: `loadKey{ModelPath, NCtx, NThreads, NBatch,
   KVCacheType}` — só recarrega o modelo/contexto quando um desses campos muda. Hoje
   qualquer `max_tokens`/`temperature` diferente (ex.: toda sumarização com
   `temperature 0.3`) força um reload completo do modelo.

Esta ADR **substitui integralmente a ADR-003** — single-flight e KV `q8_0` são
mantidos sem mudança de decisão; o que muda é o que acontece entre chamadas (limpar
tudo → reaproveitar o prefixo) e a semântica de keep-alive/reload/abort.

## Alternativas
- **Persistir sempre em disco entre turnos, sem o mecanismo de RAM via `seq_cp`**:
  avaliada com medição real (não suposição) — o disco da `demetrius` não é o gargalo
  que se temia (74-137 MB/s sequencial), mas ainda impõe I/O e disputaria o mesmo page
  cache (`vm.vfs_cache_pressure`) que o mmap do modelo em 1 GB de RAM. Rejeitada como
  caminho quente; mantida só como mecanismo de sobrevivência a restart.
- **Grammar sampling para tool-calls**: fora do escopo desta ADR (é sobre correção de
  saída, não sobre reaproveitamento de KV); permanece adiado (S10).

## Consequências
- Turno típico cai de ~6,4 min para **~60–90 s** a partir do 2º turno na mesma janela
  (delta de ~100–400 tokens reprocessados em vez do prompt completo).
- Escalonamento de janela (ADR-014) deixa de custar o prompt inteiro — só o bloco de
  tools da janela maior é decodificado, e com o mecanismo de núcleos em RAM o núcleo
  da janela `full` é restaurado por `seq_cp` em milissegundos.
- `n_ctx` sobe para **3 072–4 096** quando os núcleos em RAM estão ativos (KV
  +60–120 MB) — custo pago pelo trim do SO (+200 MB, ADR-013).
- O bloco `<think></think>` do template Qwen3 diverge do texto realmente decodificado
  no turno anterior do assistente → ~50–300 tokens são re-prefillados por turno mesmo
  com cache "perfeito" — aceito como perda pequena e conhecida; experimento de
  otimização fica como possível follow-up (Trilho E), não implementado agora.
- Heartbeat/cron/sono agora também despejam o prefixo do chat ativo ao rodar
  (mitigado pelo `runstate`, ADR-016, coordenando quem roda quando, e pela restauração
  em milissegundos via `seq_cp`).
