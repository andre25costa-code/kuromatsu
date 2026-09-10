---
id: ADR-005
title: Rebrand com shim de compatibilidade (env e home dir legados)
status: accepted
version: 1
owner: André
last_updated: 2026-09-10
depends_on: []
---

# ADR-005 — Rebrand com compatibilidade

## Contexto
Rename completo: módulo `github.com/andre25costa-code/kuromatsu`, binário `kuromatsu`,
home `~/.kuromatsu`, env `KUROMATSU_*`. Há um deploy real usando `~/.picoclaw` e
`PICOCLAW_*` (docker/data) que não pode quebrar (R5).

## Decisão
1. **Env**: shim em `pkg/config/env_compat.go` — antes do bind, cada `KUROMATSU_*`
   ausente cujo `PICOCLAW_*` exista é copiado, com um aviso de deprecação.
2. **Home**: `GetHome()` prefere `KUROMATSU_HOME`, aceita `PICOCLAW_HOME` (aviso);
   sem env, usa `~/.kuromatsu`, mas se ele não existir e `~/.picoclaw` existir, **lê
   in-place** o antigo (sem cópia — barato numa máquina de 1 GB). Migração física fica
   para um comando explícito futuro (`kuromatsu migrate home`).
3. **Ordem do rename** (cada passo com `make vet && make test`): R1 module path/imports →
   R2 `cmd/` e nomes de binário → R3 env+home → R4 cosméticos (banner, README, assets).
4. Identificadores internos (ex. `NewPicoclawCommand`) **não** são renomeados na
   primeira passada — diff sem valor funcional.

## Consequências
- Deploys antigos seguem funcionando; o custo é um shim de ~40 linhas e avisos de log.
- O rebrand acontece **depois** da poda (ADR-007) para reduzir a superfície.
