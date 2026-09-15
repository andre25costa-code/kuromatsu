---
id: S09
title: Máquina de estados — runstate
status: confirmed
version: 2
owner: André
last_updated: 2026-09-12
depends_on: [S06, S18]
---

# S09 — Máquina de estados: `runstate`

> **Status**: `confirmed` — nasce da ADR-016 (`accepted`), plano de refatoração
> `demetrius`. Este item estava `n/a` até 2026-09-11 (nenhuma entidade nova com
> máquina de estados; as máquinas do PicoClaw — sessões, cron jobs — não eram
> alteradas). O `runstate` (`pkg/runstate.Engine`, processo-wide, um único
> `Default()`) muda isso: é a primeira máquina de estados nova deste fork.

## Modelo: bitmask com refcount, não FSM de estado único

`runstate` não é uma máquina de um-estado-por-vez — é um **bitmask** (`Mode uint32`)
onde múltiplos bits podem estar simultaneamente ativos (ex.: `Inference` e `ToolExec`
juntos, durante uma chamada de tool dentro de um turno). Cada bit é **refcontado**
(`Inc/Dec`), então o mesmo bit pode ter múltiplos "titulares" concorrentes — ele só
desliga quando o refcount volta a 0.

| Bit | Valor | Significado | Gate nas transições? |
|---|---|---|---|
| `Idle` | `0` | Nenhum outro bit ativo | Precondição de `TryEnterDream` |
| `Inference` | `1<<0` | Uma chamada ao modelo (usuário, heartbeat ou cron) está decodificando | Preempta `Dream` |
| `ToolExec` | `1<<1` | Uma tool está executando | — |
| `Dream` | `1<<2` | Evolução ou modo dormir ocupam o "inconsciente" | Só entra via `TryEnterDream` quando `Idle` |
| `Suspended` | `1<<3` | Memguard (S18/ADR-017) suspendeu novas entradas por pressão de memória | Bloqueia `Inference`/`ToolExec`/`Dream` novos (`ErrBusy`) |
| `Reflex` | `1<<4` | Um reflexo (S16) está executando | — |
| `Throttled` | `1<<5` | **Informativo apenas** — CPU do e2-micro estrangulada (créditos de burst esgotados) | Nenhum — só log/estado/telemetria |

## Transições permitidas

| De | Para | Gatilho | Pré-condição |
|---|---|---|---|
| `Idle` | `+Inference` | `Inc(Inference)` (turno de usuário, heartbeat ou cron) | Nenhuma — sempre permitido, concorrente com `ToolExec`/`Reflex` |
| `Idle` | `+Dream` | `TryEnterDream(parent)` (evolução ou sono, S30/ADR-018) | `Snapshot() == Idle` (nenhum outro bit ativo) e não `Suspended` |
| qualquer com `Dream` | `Dream` removido, `+Inference` | `Inc(Inference)` chamado enquanto `Dream` ativo | Cancela `dreamCancel`; o bridge chamador re-agenda a tentativa (evolução: ~2 min; sono: até o deadline da janela) |
| qualquer | `+ToolExec` | `Enter(ToolExec)`/`defer release()` no topo de `ExecuteTools` | Nenhuma — concorrente com `Inference` |
| qualquer | `+Reflex` | `Enter(Reflex)` em `reflex.go` | Nenhuma |
| qualquer | `+Suspended` | `Suspend()` (memguard, sob pressão PSI) | `full > 10%` sustentado por ≥30 s |
| `Suspended` | `Suspended` removido | `Resume()` (memguard) | `full < 5%` sustentado por ≥30 s |
| qualquer | `+Throttled` (informativo) | Vigilante periódico lê `/proc/stat` | `steal% > 50%` sustentado por ≥60 s |
| `Throttled` | `Throttled` removido | Vigilante periódico | `steal% < 25%` |

**Transições não listadas são proibidas.** Em particular: não existe caminho para
entrar em `Dream` quando qualquer outro bit (exceto o próprio `Idle`) está ativo — uma
tentativa nessas condições retorna `ErrBusy`, nunca força a entrada.

## Invariantes

- **Refcount, não booleano**: `Inc(bit)` incrementa um contador por bit; o bit só
  desliga (`Dec` chega a 0) quando todos os titulares liberaram. Todo ponto de entrada
  usa `defer release()` para evitar "bit vazado" (nunca liberado).
- **`Inference` sempre preempta `Dream`**: não existe um modo onde o sono/evolução
  bloqueiem um turno de usuário — o inverso é que precisa de retry.
- **`Suspended` é um veto, não um bit comum**: enquanto ativo, nenhuma nova entrada de
  `Inference`/`ToolExec`/`Dream` é aceita (chamadas recebem `ErrBusy`); ver estratégia
  de retry/degradação em S17.
- **`Throttled` nunca gate nada** — é só um sinal para telemetria (S34) e para o
  operador humano entender por que um turno demorou mais do que o esperado.

## Cross-referências

- **ErrBusy e retry por chamador** (evolução re-tenta em ~2 min; sono re-tenta até o
  deadline da janela; heartbeat pula e não enfileira): estratégia completa em S17.
- **Exposição do estado** (`run/state`, `sd_notify`, `/health`, `sysmon state`): S34.
- **Decisão de arquitetura**: ADR-016. **Gate de memória que aciona `Suspend`/`Resume`**:
  ADR-017 (memguard).
