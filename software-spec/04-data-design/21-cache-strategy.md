---
id: S21
title: Cache de prefixo do KV — estratégia e consistência
status: confirmed
version: 2
owner: André
last_updated: 2026-09-12
depends_on: [S06, S18]
---

# S21 — Cache de prefixo do KV

> **Status**: `confirmed` — ADR-015 (`accepted`), plano `demetrius`. Este item estava
> `n/a` ("sem camada de cache adicional; o KV cache é interno ao runtime de inferência
> e está documentado em S18/ADR-003"). Isso deixa de ser verdade: o cache de prefixo
> entre chamadas e os núcleos de janela em RAM passam a ser uma camada de cache de
> primeira classe, com chave, invalidação e nível de consistência próprios — mesmo
> sendo interna ao processo de inferência (não uma camada de cache de dados de
> negócio no sentido clássico de S21). É reinterpretado aqui porque é exatamente o
> que este item do checklist pede: cache layer + estratégia de invalidação/leitura +
> nível de consistência, aplicado ao KV cache do llama.cpp em vez de a um Redis.

## O que é cacheado

| Camada | Onde vive | Granularidade |
|---|---|---|
| **Prefixo entre chamadas** | KV cache do processo (`kvTokens []C.llama_token`, sequência 0) | Todo turno — o maior prefixo comum entre o que já está decodificado e o prompt novo |
| **Núcleo de janela** | KV cache unificado, sequências `1..N` (N=2 padrão) | Por janela de foco (`options["focus_window"]`) — o bloco system+tools+histórico-antigo que é comum entre turnos da mesma janela |
| **Snapshot em disco** (opcional, só restart) | `kv_state_dir` (arquivo por chave/hash) | Núcleos das janelas mais usadas, salvos quando `runstate` está `Idle` |

## Chave de cache

- Prefixo entre chamadas: implícito — é o próprio `kvTokens` atual comparado
  byte-a-byte (token-a-token) com o prompt novo (`commonPrefixLen`).
- Núcleo de janela: `CacheKey = options["focus_window"]` (string vazia = sem cache de
  núcleo — comportamento antigo). Vem do roteador de foco (S16/ADR-014).
- Snapshot em disco: chave + hash do conteúdo — arquivos de uma chave com hash
  divergente do que está em disco são apagados (evita servir um núcleo desatualizado
  depois de o texto do system prompt mudar).

## Estratégia de leitura/escrita

- **Leitura**: a cada `completion`, calcula o prefixo comum com o KV atual; se
  `nCommon == len(tokens)`, decodifica pelo menos 1 token (precisa de logits); se
  `nCommon == 0`, `llama_memory_clear` e reprocessa tudo (cache miss total — nunca é um
  erro, só mais lento).
- **Escrita**: todo token efetivamente decodificado com sucesso entra em `kvTokens`
  (sufixo do prompt + tokens amostrados decodificados). EOG e o último token em
  `MaxPredict` **não** são decodificados, então não entram.
- **Compartilhamento de núcleo**: `llama_memory_seq_cp(mem, 0, k, 0, nCore)` — no KV
  unificado isso não copia dados, só marca células como pertencentes também à
  sequência `k` (zero custo de cópia, mesma técnica do `llama-server` para slots).

## Invalidação e eviction

| Evento | Ação |
|---|---|
| Erro ou abort durante decode | `llama_memory_clear` total + `kvTokens = nil` — nunca um estado parcial |
| Mudança de `loadKey` (modelo/ctx/threads/batch/tipo de KV) | Reload completo — cache anterior descartado junto |
| Janela nova precisa de espaço e já há N núcleos cacheados | LRU por janela: `llama_memory_seq_rm(k, -1, -1)` libera só as células daquela janela |
| Arquivo de snapshot em disco com hash divergente do conteúdo atual | Apagado — nunca servido |
| Heartbeat/cron/sono rodam durante uma conversa ativa | Despejam o prefixo do chat ativo (sequência 0) — mitigado por restaurar o núcleo da janela via `seq_cp` em milissegundos na volta |

## Nível de consistência

**Best-effort, nunca afeta correção** — um cache miss (total ou parcial) só torna o
turno mais lento (reprocessa mais tokens), nunca produz uma resposta errada: o texto
efetivamente enviado ao modelo é sempre o prompt completo e correto para aquele turno,
independentemente de quanto dele veio do cache. Não há "leitura suja" possível porque
não há dado de negócio no cache — só estado interno de tokenização já processada.

## Persistência em disco: gate de aceite

A persistência em disco (item opcional, só usado no restart do processo) é gated por
medição, não por suposição: `load_ms` (tempo pra recarregar um núcleo do disco) ≤5s e
sem pico de `iowait` (`vmstat`) >30% durante o boot. Medição real na `demetrius`
(`dd` sequencial, disco `pd-standard`): 74–137 MB/s, `iowait` 0 — dentro do esperado
para uma leitura sequencial única de 45–90 MB por núcleo. Se esse gate falhar em
produção, `kv_state_dir: "off"` desliga só a parte de disco — o mecanismo em RAM
(núcleos via `seq_cp`) continua funcionando normalmente, já que não depende do disco.

## Cross-referências

- **Mecanismo completo (API C, invariantes de `kvTokens`)**: ADR-015.
- **Como a janela de foco decide a `CacheKey`**: S16/ADR-014.
- **Consumo em telemetria** (`cached_tokens`, hit ratio): S34.
