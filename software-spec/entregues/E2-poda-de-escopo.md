---
id: E2
title: Poda de escopo (web launcher, canais, hardware, docs legados)
status: done
frs: [FR-007]
adrs: [ADR-007]
commits: [c9b2f9db, 45bc7cac, 2311950f, cb895bec, ef30e18a]
last_updated: 2026-09-11
---

# E2 — Poda de escopo

## O que foi entregue

Remoção, em commits independentes que compilam (`go build`/`go vet` limpos a
cada passo), de tudo que a ADR-007 marcou como fora do escopo do fork:

1. `c9b2f9db` — launcher web (`web/`, Go+React, ~31k linhas) e todo o
   tooling de build associado (Dockerfiles, script do app macOS, workflow do
   DMG, targets do Makefile/goreleaser).
2. `45bc7cac` — 17 dos 22 canais de chat herdados (`deltachat`, `dingtalk`,
   `discord`, `feishu`, `irc`, `line`, `maixcam`, `matrix`, `mqtt`, `onebot`,
   `qq`, `slack`, `slack_webhook`, `teams_webhook`, `vk`, `wecom`, `weixin`);
   mantidos `pico`, `pico_client`, `telegram`, `whatsapp`, `whatsapp_native`.
3. `2311950f` — tools de hardware Sipeed (`pkg/tools/hardware`: i2c/spi/serial)
   e o canal `maixcam` (câmera Sipeed).
4. `cb895bec` — `docs/` legado do PicoClaw (213 arquivos, 10 idiomas) e
   `examples/pico-echo-server`.
5. `ef30e18a` — registro das métricas de baseline (tamanho de binário e
   contagem de pacotes antes/depois) no S40, medidas em git worktrees isolados
   presos aos commits `d17b2150` (fim do E1) e `cb895bec` (fim do E2) para não
   contaminar com trabalho concorrente não commitado.

## FRs cobertas

| FR | Descrição | ACs | Status |
|---|---|---|---|
| FR-007 | Poda de escopo | AC-007-1, AC-007-2 | Verificado |

AC-007-1 (cada commit de remoção compila limpo): atestado pelas próprias
mensagens de commit e confirmado nesta auditoria por inspeção — `web/` e
`pkg/tools/hardware/` não existem mais no working tree; `pkg/channels/`
contém hoje só os 4 diretórios esperados (`pico`, `telegram`, `whatsapp`,
`whatsapp_native`, além do `pico_client` embutido no manager). AC-007-2
(medições no S40): valores presentes — binário 50.034.076 → 38.999.406 bytes
(≈22% menor), pacotes Go 110 → 86 (≈22% menos).

## Commits

| Commit | Mensagem | Diff |
|---|---|---|
| `c9b2f9db` | chore(scope): remove the web launcher (picoclaw-launcher) | 7 arquivos, +3/-204 |
| `45bc7cac` | chore(scope): remove unused chat channels, keep pico/telegram/whatsapp | 97 arquivos, +62/-26887 |
| `2311950f` | chore(scope): remove Sipeed hardware tools (i2c/spi/serial) | 23 arquivos, +0/-2504 |
| `cb895bec` | chore(scope): remove legacy PicoClaw docs and unused examples | 218 arquivos, +6/-47856 |
| `ef30e18a` | docs(spec): record E2 pruning baseline metrics | 1 arquivo, +9/-2 |

Nota: o grosso da remoção de `web/` (diretório em si, Dockerfiles do launcher,
script macOS, workflow do DMG) foi de fato commitado dentro de `5d2fe0fe`
(E4) por sobreposição de staging entre dois trabalhos concorrentes na mesma
branch — `c9b2f9db` carrega só as referências residuais que ainda quebrariam
build/CI. Ver a nota do próprio commit `c9b2f9db`.

## Arquivos-chave (confirmados ausentes nesta auditoria)

`web/` (ausente), `pkg/tools/hardware/` (ausente); `pkg/channels/` (só
`pico`, `telegram`, `whatsapp`, `whatsapp_native`); `docs/` e `examples/`
(ausentes).

## Como verificar

`git show --stat <commit>` para cada linha da tabela; `software-spec/09-appendix/40-references.md`
(tabela de métricas de baseline); listar `pkg/channels/` e confirmar que só
restam os 4 diretórios esperados.

## Notas / pendências conhecidas (fora do escopo do E2 em si)

- O próprio commit `cb895bec` já registrou que o `README.md` raiz e
  `pkg/channels/README*.md` ainda descrevem extensivamente o launcher web e os
  17 canais removidos (~40+ links/exemplos mortos) — deferido para E8
  (consolidação de docs, ADR-010).
- Esta auditoria observou que `workspace/skills/hardware/` e
  `workspace/skills/picoclaw-agent/` (skills do agente, distintas do código Go
  removido em `pkg/tools/hardware`) ainda existem em disco. Isso está fora do
  AC-007-1 (que é sobre o código Go), mas é um resíduo do mesmo espírito de
  poda; o plano de refatoração aprovado em 2026-09-11 já atribui essa limpeza
  ao agente `code-analyst` (não a este entregável). Ver `BACKLOG.md`.
