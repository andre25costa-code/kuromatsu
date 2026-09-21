# Orçamento de RAM e motor de estados

Fonte real: `pkg/runstate/`, `pkg/sysinfo/`, ADR-015/016/017.

## `runstate` — motor de estados (bitmask com refcount)

`pkg/runstate.Engine`: bits `Idle=0`, `Inference`, `ToolExec`, `Dream`,
`Suspended`, `Reflex`, `Throttled` (informativo — ligado quando `steal` de
CPU passa de 50% por ≥60s, sem ação automática, só log/telemetria).
`Inc(bit)`/`release()` contam referência; `Inc(Inference)` com `Dream`
ativo **cancela** o sono em andamento (inferência do usuário sempre
preempta consolidação noturna). `TryEnterDream` só entra se `Idle`.

Publicado em três lugares: arquivo `$KUROMATSU_HOME/run/state`
(`bits=… names=… since=…`), `sd_notify` (status pro systemd), log de
transição. `sysmon state` e `/health` também expõem o estado atual.
`runstate.enabled: false` (compat) = nenhum arquivo/sd_notify/skip de
heartbeat — o resto do sistema funciona exatamente como sem runstate.

## Orçamento de espera pra suspensão (`runstate_resume_wait_secs`)

Uma suspensão do memguard (ver abaixo) é classificada como erro
transitório de rate-limit — mas a janela de sustentação real do memguard é
de dezenas de segundos, muito maior que o backoff genérico de retry
(`max_llm_retries`/`llm_retry_backoff_secs`, ~2-4s). `waitForRunstateResume`
dá um orçamento de espera **separado**, configurável, que escuta a
transição real via `Engine.Subscribe()` em vez de aplicar o backoff fixo —
cada tentativa que colide com suspensão ainda consome um slot de retry, mas
pode esperar até `runstate_resume_wait_secs` em vez de só 2-4s.

## `memguard` — guarda de memória (ADR-017)

Lê `pkg/sysinfo` (cgroup v2/v1, `/proc/meminfo`, PSI, `/proc/stat`) —
pacote único reusado também por `pkg/tools/sysmon.go`, sem duplicar parser.

- `Plan(estimativa) = min(cgroup, MemTotal) - modelo - KV - compute - margem`
  → vira `debug.SetMemoryLimit` do Go (`GOMEMLIMIT` dinâmico, não estático
  e descolado da realidade) + `debug.SetGCPercent` mais agressivo.
- `PreLoad`: se `MemAvailable` não cobrir KV+compute+margem, erro
  classificado como "overloaded" → cai no retry transitório normal, sem
  crashar.
- Vigilante de PSI (`/proc/pressure/memory`): `some > limiar` dispara
  `runtime.GC()`+`FreeOSMemory()` (no máx. 1×/min); `full > limiar` por N
  segundos sustentados suspende (`Suspend()`) e descarrega o modelo nativo
  (`localllm.UnloadAll()`); recupera quando a pressão cai por N segundos.
- **Sem Linux/PSI, desliga com aviso** — não crasha, mas perde a rede de
  segurança contra OOM que o resto do desenho pressupõe. Verificar
  `cat /proc/pressure/memory` no hardware real (ex.: Raspberry Pi) antes de
  confiar nisso em produção; PSI é padrão nos kernels 6.x recentes, mas
  precisa ser confirmado, não assumido.

## Cache de prefixo e núcleos de janela (B1/B2, `pkg/providers/localllm/`)

O KV cache do modelo nativo não é limpo a cada chamada — o prefixo comum
entre a chamada atual e a anterior é reaproveitado
(`llama_memory_seq_rm` só do que divergiu). Com `core_cache_parking: true`,
o núcleo (system prompt + catálogo de tools) de até 2 janelas fica
"estacionado" no KV (`llama_memory_seq_cp`, não copia dado, só marca
células como pertencentes também à sequência do núcleo) — trocar de janela
restaura o núcleo em milissegundos em vez de reprocessar do zero.

**Núcleo estacionado não é contexto reservado**: se um novo prompt não
cabe porque núcleos antigos ainda ocupam espaço, o engine evicta os núcleos
inativos em ordem LRU antes de rejeitar por overflow (achado real de
produção, incidente NATIVE-02 do audit de 2026-09-17 — sem essa eviction,
a virada de dia no system prompt, que gera um núcleo novo, ficava
permanentemente bloqueada depois que o cache enchia). O núcleo ativo nunca
é evictado; overflow real (nem evictando dá espaço) continua erro.

`keep_alive_secs: -1` (nunca descarrega o modelo entre chamadas) é seguro
com o memguard ligado — ele descarrega sob pressão real de memória
independente desse campo.
