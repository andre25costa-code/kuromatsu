---
id: ADR-010
title: Documentação consolidada numa única skill (kuromatsu-docs), embutida via workspace
status: accepted
version: 1
owner: André
last_updated: 2026-09-10
depends_on: []
---

# ADR-010 — Documentação em skill única

## Contexto
O PicoClaw espalha 213 arquivos de docs em 10 idiomas + READMEs por pasta. O usuário
quer o inverso: uma única fonte, em PT-BR, que sirva a três papéis — (a) referência de
desenvolvimento nas sessões com Claude, (b) guia do usuário exposto pelo próprio agente,
(c) insumo do ciclo de auto-aperfeiçoamento (modo dormir lê a mesma fonte que documenta
o comportamento esperado).

## Decisão
1. Fonte canônica: **`workspace/skills/kuromatsu-docs/`** — versionada, embutida no
   binário pelo embed do onboard (vira skill do agente automaticamente) e navegável por
   árvore de decisão no `SKILL.md` (padrão clawddocs, porém 100% offline/local).
2. Um arquivo canônico por tópico (config, modelos, canais, tools, cron/heartbeat,
   sono, RAM, deploy); qualquer outra menção é link (唯一归属 — princípio de dono único).
3. `.claude/skills/kuromatsu-docs/` é apenas um shim apontando para a canônica.
4. `README.md` raiz fica curto (identidade + quickstart) e aponta para a skill.
   `software-spec/` continua separado: spec é decisão/contrato, skill é manual.

## Alternativas
- `docs/` tradicional: não chega ao agente em runtime nem ao onboard — rejeitado.
- Docs embutidos direto no binário via comando `kuromatsu docs`: possível follow-up;
  o embed do workspace já os coloca dentro do binário de qualquer forma.

## Consequências
- Docs viajam com o binário e com o workspace do usuário; uma edição, três consumidores.
- Disciplina: nenhum README novo por pasta (AC-009-3 audita).
