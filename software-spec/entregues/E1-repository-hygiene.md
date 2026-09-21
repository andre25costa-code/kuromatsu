---
id: E1
title: Higiene do repositório (submodule, download verificado, workspace template)
status: done
frs: [FR-006, FR-012]
adrs: [ADR-009]
commits: [d17b2150]
last_updated: 2026-09-11
---

# E1 — Higiene do repositório

## O que foi entregue

- `llama.cpp/` registrado como git submodule de `PrismML-Eng/llama.cpp`, pinado
  em `d8f26eec7` (ADR-009).
- `models/*.gguf` e artefatos de build C ignorados no git (limite de 100 MB do
  GitHub, restrição C2 de S06).
- `scripts/download-model.sh` + `make model-download`: baixa o GGUF, valida
  SHA256, é idempotente (não rebaixa se já íntegro) e rejeita com erro claro em
  caso de divergência.
- `onboard_workspace_embed.go` trocado de um embed genérico de `workspace` para
  uma lista explícita de arquivos — conteúdo não rastreado/pessoal colocado sob
  `workspace/` nunca pode vazar para o binário compilado (BR-003).
- `workspace/{AGENT,SOUL,USER,HEARTBEAT}.md` e `workspace/memory/MEMORY.md`
  reescritos em PT-BR como uma persona genérica de partida ("Kuro"); o hub de
  estudos pessoal do André já tinha backup idêntico em `docker/data/workspace/`
  (gitignorado) e permanece exclusivamente lá.

## FRs cobertas

| FR | Descrição | ACs | Status |
|---|---|---|---|
| FR-006 | Workspace template + embed seguro | AC-006-1, AC-006-2, AC-006-3 | Verificado |
| FR-012 | Download verificado do modelo | AC-012-1, AC-012-2, AC-012-3 | Verificado |

Verificação nesta auditoria: `workspace/` contém apenas os arquivos do template
+ `skills/` (confirmado via listagem de diretório — nenhum `hub_core`, `data/`
ou sessão); `scripts/download-model.sh` contém `SHA256_EXPECTED`, a função
`checksum()`, o skip idempotente quando o checksum já bate, e a rejeição com
mensagem de erro quando diverge (confirmado lendo o script).

## Commits

| Commit | Mensagem | Diff |
|---|---|---|
| `d17b2150` | chore(repo): repository hygiene for the Kuromatsu fork (E1) | 11 arquivos, +150/-72 |

## Arquivos-chave

`.gitmodules`, `llama.cpp` (submodule), `scripts/download-model.sh`,
`Makefile` (target `model-download`), `onboard_workspace_embed.go`,
`workspace/{AGENT,SOUL,USER,HEARTBEAT}.md`, `workspace/memory/MEMORY.md`.

## Como verificar

`git show --stat d17b2150`; `cat scripts/download-model.sh` (SHA256 hardcoded,
hoje já confirmado como fonte oficial — ver S25/E7); `git ls-files workspace/`.

## Notas

Nenhum gap conhecido para este entregável. A confirmação da fonte oficial do
GGUF (S25) veio depois, em E7 (commit `b2912789`), mas o mecanismo de
verificação em si (SHA256 hardcoded + checagem) já nasceu correto aqui.
