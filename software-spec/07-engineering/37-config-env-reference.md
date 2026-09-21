---
id: S37
title: Referência de configuração e variáveis de ambiente
status: confirmed
version: 1
owner: André
last_updated: 2026-09-21
depends_on: [E8]
---

# S37 — Referência de configuração e variáveis de ambiente

> **Status**: `confirmed`. Estava `missing` com a nota "tabela de
> config/env (`KUROMATSU_*`) deve ser gerada após o E3 (rename) e mantida
> na skill `kuromatsu-docs` (E8), não aqui — evitar dupla fonte." O E3
> (rename `PICOCLAW_*`→`KUROMATSU_*`) já está entregue, e o E8
> (`workspace/skills/kuromatsu-docs/`) foi implementado nesta rodada
> (2026-09-21) — a tabela existe, no lugar que a nota original já previa.
> Este capítulo, de propósito, não repete a tabela — só aponta pra ela.

## Onde está de fato

`workspace/skills/kuromatsu-docs/references/config.md` documenta:

- Onde `config.json`/`.security.yml` moram e a ordem de resolução
  (`KUROMATSU_CONFIG` → `KUROMATSU_HOME`/`config.json`).
- O padrão de nome das variáveis `KUROMATSU_*` (uma por campo com tag
  `env:"..."` em `pkg/config/*.go`) e o comando pra listar as 119
  existentes hoje sempre atualizado, em vez de uma tabela hardcoded que
  ficaria errada no primeiro campo novo:

  ```bash
  grep -oE 'env:"KUROMATSU_[A-Z0-9_]+"' pkg/config/*.go | sed 's/.*env:"//;s/"//' | sort -u
  ```

- As validações que rodam em `LoadConfig` (`ValidateSleep`,
  `ValidatePlatformPaths`, `ValidateFocus`).
- A compatibilidade legada `PICOCLAW_*` (shim, honrado quando o
  `KUROMATSU_*` equivalente não está setado — AC-008-3).

## Por que uma tabela gerada, não hardcoded, é a decisão certa

119 variáveis hoje, crescendo a cada campo de config novo com tag `env`.
Uma tabela copiada à mão neste capítulo (ou em qualquer lugar) ficaria
desatualizada na primeira PR que adiciona um campo — exatamente o tipo de
alegação sem número real que o audit de 2026-09-16 tratou como critério de
qualidade em todo o resto do projeto (ver O4/KR4.1 do plano de
refatoração). O comando `grep` acima é reproduzível e nunca mente.

## Cross-referências

- **Skill completa**: `workspace/skills/kuromatsu-docs/` (E8, ADR-010).
- **Schema de `Config`**: `pkg/config/config.go`.
