---
id: ADR-016
title: Motor de estados `runstate` (bitmask com refcount) como gate único de agendadores e ponte com o SO
status: accepted
version: 1
owner: André
last_updated: 2026-09-12
depends_on: []
---

# ADR-016 — Motor de estados `runstate`

## Contexto
Heartbeat, cron, evolução (`pkg/evolution`) e o futuro modo dormir (`pkg/sleep`,
ADR-006/018) hoje não têm nenhum sinal compartilhado de "o agente está ocupado" — cada
um pode disputar o único `llama_context` (ADR-003/015) com um turno de usuário ativo,
silenciosamente. O André pediu explicitamente um motor de estados e integração com o
SO (systemd, shell, cron) — agente e máquina "indistinguíveis".

A ideia de **matar processos concorrentes à força durante a inferência** foi avaliada
e **rejeitada como mecanismo dinâmico**: na VM dedicada (`demetrius`) não sobra
processo relevante para matar em tempo real sem risco (matar `google-guest-agent`
derruba o SSH). O ganho real está no **trim permanente** (Trilho 0/ADR-013) e em
prioridade **estática** no systemd (`CPUWeight`, `Nice`, `OOMScoreAdjust`) — o arquivo
de estado desta ADR fica disponível para scripts futuros decidirem ações pontuais
fora do binário, se um dia fizer sentido (ex.: pausar `apt` durante inferência).

## Decisão
1. **`pkg/runstate`** — pacote-folha (dependências só de `pkg/logger`,
   `pkg/fileutil`): `Mode uint32`, bits via `1 << iota`: `Idle=0`, `Inference`,
   `ToolExec`, `Dream`, `Suspended`, `Reflex`, `Throttled`.
2. **`Throttled` é puramente informativo** — ligado pelo vigilante quando `steal` (de
   `/proc/stat`) fica >50% sustentado por ≥60s, desligado abaixo de 25%. Não gate
   nenhuma transição; existe só para log/estado/telemetria (evidenciar o throttling do
   modelo de créditos de burst do e2-micro, ver S06 R7).
3. **Bits refcontados**: `Inc(bit)/Dec(bit)`, atalho `Enter(bit) → release()`.
   `TryEnterDream(parent) (ctx, release, ok)` só sucede quando o estado atual é
   exatamente `Idle` (nenhum outro bit ativo) e não `Suspended`. `Inc(Inference)`
   enquanto `Dream` está ativo **cancela** o `dreamCancel` — inferência de usuário
   sempre preempta sono/evolução; o bridge responsável reagenda a tentativa.
4. **Publicadores** (todos opcionais, config `runstate.enabled`): arquivo
   `$KUROMATSU_HOME/run/state` (escrita atômica via `fileutil.WriteFileAtomic`,
   formato `bits=… names=… since=…`); `sd_notify` via datagrama unix em
   `$NOTIFY_SOCKET` (`STATUS=<names>`, `SdReady()`/`SdStopping()` no boot/shutdown do
   gateway); log de toda transição.
5. **Ponte com ferramentas existentes**: ação `sysmon state` (leitor injetado); campo
   `state:{bits,names}` em `/health` (aditivo — JSON existente não muda sem isso
   configurado).
6. **`Suspend()`/`Resume()`**: acionados pelo guarda de memória (ADR-017) sob pressão
   — nenhuma nova entrada (`Inference`/`ToolExec`/`Dream`) é aceita enquanto
   `Suspended`; chamadas recebem `ErrBusy`.
7. **Hooks nos pontos de entrada existentes** (sem mudar a lógica de negócio deles):
   `Inference` em `agent_utils.go:596-611`/`pipeline_llm.go:187-188`/
   `turn_coord.go:492`; `ToolExec` no topo de `ExecuteTools`
   (`pipeline_execute.go:111-121`); `Reflex` em `reflex.go`; `Dream` (evolução) via um
   `dreamGate` que envolve o runtime passado a `NewColdPathRunnerWithErrorHandler`
   (zero mudança em `pkg/evolution`); `Dream` (sono) via o mesmo mecanismo (ADR-018);
   heartbeat via `SetShouldSkip(fn)` — checado no topo de `executeHeartbeat`, que
   **pula** (loga) em vez de enfileirar quando ocupado.
8. **Config top-level** `runstate: {enabled (false por padrão — compatibilidade;
   true na demetrius), state_file, sd_notify (true), skip_heartbeat_when_busy (true)}`.

## Alternativas
- **Kill dinâmico de processos do SO durante a inferência**: rejeitada — nenhum
  processo relevante para matar sem quebrar o acesso à VM (SSH via
  `google-guest-agent`); o ganho real já foi obtido de outra forma (trim permanente +
  prioridade estática do systemd, ADR-013).
- **Um mutex simples (só serializar acesso)** em vez de bitmask com múltiplos bits:
  insuficiente — não distingue **por que** o sistema está ocupado, e essa distinção é
  o que permite decisões diferentes por chamador (heartbeat pula; sono tenta de novo
  em 5 min; memguard suspende tudo).

## Consequências
- Elimina a disputa silenciosa pelo único `llama_context`; heartbeat nunca colide com
  um turno de usuário ativo.
- Base observável e auditável pelo operador humano: `/health`, `run/state`,
  `systemctl status` mostram o mesmo estado.
- Risco de "bit vazado" (nunca liberado, travando o heartbeat para sempre) — mitigado
  por refcount + `defer release()` em **todo** ponto de entrada, e `/health` expõe
  `since` (tempo no estado atual) para detectar isso rapidamente em produção.
- Com `runstate.enabled=false` (padrão fora da `demetrius`): zero overhead, nenhuma
  goroutine nova, comportamento idêntico ao atual — compatibilidade total.
