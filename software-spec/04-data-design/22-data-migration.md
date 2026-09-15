---
id: S22
title: Migração e transformação de dados
status: confirmed
version: 1
owner: André
last_updated: 2026-09-11
depends_on: [S06, S32]
---

# S22 — Migração e transformação de dados

Única migração de dados do projeto: realocação do diretório home do usuário de
`~/.picoclaw` para `~/.kuromatsu`, parte do rebrand (ADR-005). Não há schema de
banco de dados novo nem migração de tabelas — S19/S20 são `n/a` (sem entidades
de dados novas; ver `spec-coverage.yaml`).

## Migração de home dir

| Aspecto | Decisão |
|---|---|
| Origem | `~/.picoclaw` (ou `$PICOCLAW_HOME`) — deploys existentes antes do rebrand |
| Destino | `~/.kuromatsu` (ou `$KUROMATSU_HOME`) |
| Mecanismo | **Leitura in-place, sem cópia**: `GetHome()` prefere `KUROMATSU_HOME`; sem env, usa `~/.kuromatsu`; se este não existir e `~/.picoclaw` existir, lê o antigo diretamente no lugar (barato numa máquina de 1 GB — restrição C1 de S06) |
| Backfill | Nenhum — o mesmo conteúdo é lido como está; não há campo/formato a converter |
| Validação | Não aplicável — nenhuma transformação de esquema ocorre |
| Rollback | Trivial — nenhuma cópia foi feita; reapontar `KUROMATSU_HOME`/`PICOCLAW_HOME` desfaz |
| Migração física (futuro) | Comando explícito `kuromatsu migrate home` copiaria/moveria o conteúdo definitivamente — **não implementado ainda** |

## Migração de variáveis de ambiente

Mesmo princípio aplicado a env vars: o shim `pkg/config/env_compat.go`
(`applyLegacyEnvCompat`, ADR-005) copia, uma vez por processo, qualquer
`PICOCLAW_*` ainda definida para a `KUROMATSU_*` equivalente quando esta não foi
definida — genérico sobre todo campo de config com tag `env` (não uma tabela fixa
de nomes). Loga aviso de deprecação a cada cópia efetuada.

## Decisão de arquitetura

ADR-005 (rebrand com shim de compatibilidade) contém a decisão e as alternativas
consideradas. Ambiente de deploy onde a migração ocorre: S32 (que referencia este
capítulo em vez de repetir o conteúdo — 唯一歸屬).
