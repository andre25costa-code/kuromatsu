---
id: E0
title: Bootstrap do Spec-Driven Delivery
status: done
frs: []
adrs: [ADR-001, ADR-002, ADR-003, ADR-004, ADR-005, ADR-006, ADR-007, ADR-008, ADR-009, ADR-010]
commits: [0e488544]
last_updated: 2026-09-11
---

# E0 — Bootstrap do Spec-Driven Delivery

## O que foi entregue

Criação da estrutura `software-spec/` seguindo a metodologia da skill
`software-spec-writing`: manifest (`spec.manifest.yaml`), vetor de cobertura
dos 40 itens (`spec-coverage.yaml`), e os capítulos com conteúdo já confirmado
na época — contexto de negócio, escopo, FR-001..012 com critérios de aceite,
arquitetura, componente de IA, NFRs, deployment e ADR-001..010. Todo o conteúdo
foi marcado como derivado do plano de refatoração já aprovado pelo André — não
há itens `draft` nesta rodada inicial.

Este entregável não implementa nenhum FR; ele é a infraestrutura documental que
torna os demais entregáveis (E1-E7) auditáveis.

## Commits

| Commit | Mensagem |
|---|---|
| `0e488544` | docs(spec): bootstrap Spec-Driven Delivery for Kuromatsu (E0) |

26 arquivos criados, 1126 linhas (ver `git show --stat 0e488544`).

## Arquivos-chave

`software-spec/{INDEX.md,spec.manifest.yaml,spec-coverage.yaml}`,
`01-business/{01-background,02-glossary,04-goals,06-assumptions}.md`,
`02-business-design/{08-business-rules,10-scope,11-requirements}.md`,
`03-system-design/{13-architecture,14-modules,18-ai-component}.md`,
`06-infrastructure/{29-nfr,32-deployment}.md`, `09-appendix/40-references.md`,
`07-engineering/adr/ADR-001..010-*.md`.

## Como verificar

`git show --stat 0e488544` e os próprios arquivos listados acima (todos ainda
existem e continuam com `status: confirmed`, embora vários tenham sido
corrigidos/versão-incrementados por auditorias posteriores, incluindo esta).

## Notas

- Esta auditoria (2026-09-11) encontrou e corrigiu quatro inconsistências
  herdadas do E0 que haviam ficado obsoletas por eventos posteriores (S25
  resolvida mas ainda citada como TBD em 4 lugares; `INDEX.md` citando
  "ADR-001..010" quando já existem ADR-011/012; `spec-coverage.yaml` S22
  apontando para uma ADR em vez de capítulo próprio; S40 com duas linhas de
  baseline vazias sem explicação). Ver `BACKLOG.md` e os commits desta
  auditoria para o detalhe.
