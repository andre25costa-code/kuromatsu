---
id: S36
title: Convenções de código e commit
status: confirmed
version: 1
owner: André
last_updated: 2026-09-21
depends_on: []
---

# S36 — Convenções de código e commit

> **Status**: `confirmed`, mas deliberadamente curto. Este item estava
> `missing` com a nota "convenções herdadas (golangci, make check,
> Conventional Commits do upstream). Documentar apenas se divergirmos." O
> fork não divergiu do padrão herdado — o que existe hoje é `AGENTS.md`
> (raiz do repo, adicionado no audit de 2026-09-16), que já documenta essas
> convenções para quem desenvolve o Kuromatsu com Claude Code. Duplicar o
> conteúdo aqui violaria a própria regra de dono único (唯一归属) que a
> skill `kuromatsu-docs` (S37, E8) segue.

## Onde a convenção real mora

`AGENTS.md` (raiz do repositório) é o dono único de:

- Comandos de build/teste/lint (`make build/test/lint/check`), flags
  padrão (`CGO_ENABLED=0`, tags `goolm,stdjson`).
- Estilo de código (Go idiomático, `gofmt`/`gofumpt`/`goimports`/`gci`/
  `golines` a 120 colunas, ordem de import: stdlib → externo → módulo
  local).
- Convenção de nome de teste (`TestXxx` em `*_test.go`) e de teste de
  integração (`*_integration_test.go`, tag `integration`).
- Convenção de commit (Conventional Commits, imperativo em inglês:
  `fix(agent): preserve ready responses`) e de PR (branch a partir de
  `main`, template preenchido, `make check` + CI verde antes de revisão).

Este capítulo existe só para que o item apareça como resolvido no
checklist da skill `software-spec-writing`, não porque o conteúdo precisa
de uma segunda casa.

## O que divergiria disso (nenhum item hoje)

Nenhuma convenção deste fork diverge do que `AGENTS.md` descreve. Se um
dia divergir (ex.: uma regra de lint específica do Kuromatsu que o upstream
não tem), documentar a divergência aqui — não duplicar o resto.

## Cross-referências

- **Convenção completa**: `AGENTS.md` (raiz).
- **Skill de referência operacional**: `workspace/skills/kuromatsu-docs/`
  (S37/E8) — cobre o "como o sistema se comporta", não "como escrever
  código para ele".
