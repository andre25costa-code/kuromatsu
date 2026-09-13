---
id: E3
title: Rebrand PicoClaw → Kuromatsu com compatibilidade
status: done
frs: [FR-008]
adrs: [ADR-005]
commits: [7ec2c392, 5112bbda, a376f484, 1bbb7e7a]
last_updated: 2026-09-11
---

# E3 — Rebrand com compatibilidade

## O que foi entregue

Rename mecânico completo em 4 passos sequenciais (cada um com `go build`/`go
vet`/`go test` limpos antes do próximo), seguindo a ordem decidida em ADR-005:

1. **R1** (`7ec2c392`) — `go.mod` retargetado para
   `github.com/andre25costa-code/kuromatsu`; imports reescritos em 391 arquivos
   Go; `onboard_workspace_embed.go` de `package picoclaw` para `package kuromatsu`.
2. **R2** (`5112bbda`) — `cmd/picoclaw/` → `cmd/kuromatsu/`; `BINARY_NAME=kuromatsu`
   no Makefile; ids/imagens do goreleaser e Dockerfiles atualizados.
3. **R3** (`a376f484`) — prefixo de env `PICOCLAW_*` → `KUROMATSU_*` (31
   arquivos); `pkg/env.go` (`AppName="Kuromatsu"`); `pkg/config/envkeys.go`:
   `GetHome()` passa a preferir `KUROMATSU_HOME` e lê `~/.picoclaw` in-place
   quando `~/.kuromatsu` não existe; **shim novo** `pkg/config/env_compat.go`
   (`applyLegacyEnvCompat`) copia qualquer `PICOCLAW_*` ainda setada para a
   `KUROMATSU_*` equivalente, genérico sobre todo campo com tag `env`.
4. **R4** (`1bbb7e7a`) — textos de usuário: help/erros da CLI, banner ASCII
   (verde/pinheiro em vez de azul/vermelho), identidade do system prompt do
   agente e saudação padrão (`pkg/agent/context.go`, `pkg/commands/cmd_start.go`),
   User-Agent HTTP (`Kuromatsu/<version>`).

## FRs cobertas

| FR | Descrição | ACs | Status |
|---|---|---|---|
| FR-008 | Rebrand com compatibilidade | AC-008-1, AC-008-2, AC-008-3 | Verificado |

AC-008-1: confirmado lendo `go.mod` (`module github.com/andre25costa-code/kuromatsu`)
e via R2 (binário `kuromatsu`). AC-008-2/AC-008-3: confirmado lendo o histórico
de `pkg/config/env_compat.go` (`git log --follow`, adicionado em `a376f484`,
ajustado em `1bbb7e7a`) — a lógica descrita em ADR-005 está implementada e
commitada, não apenas desenhada.

## Commits

| Commit | Mensagem | Diff |
|---|---|---|
| `7ec2c392` | refactor(rebrand): retarget the module path (E3-R1) | 392 arquivos, +971/-971 |
| `5112bbda` | refactor(rebrand): rename the cmd dir, binary, and build artifacts (E3-R2) | 99 arquivos, +115/-111 |
| `a376f484` | refactor(rebrand): rename the env prefix and home dir, with legacy compat (E3-R3) | — |
| `1bbb7e7a` | refactor(rebrand): rename user-facing text and CLI identity (E3-R4) | — |

## Arquivos-chave

`go.mod`, `cmd/kuromatsu/`, `pkg/env.go`, `pkg/config/envkeys.go`,
`pkg/config/env_compat.go`, `pkg/updater/*` (URLs de release apontadas para
`andre25costa-code/kuromatsu`).

## Como verificar

`grep "^module" go.mod`; `git log --oneline --follow -- pkg/config/env_compat.go`;
`git show --stat a376f484` / `1bbb7e7a`.

## Notas / exclusões deliberadas (não são gaps — são decisões registradas nos próprios commits)

- `pkg/credential/`: **intencionalmente não varrido**. A string de info do HKDF
  e o nome do arquivo `~/.ssh/picoclaw_ed25519.key` são usados para derivar e
  localizar credenciais já criptografadas de quem já rodou `auth login --enc`;
  renomear quebraria a decriptação silenciosamente. Mantido como "picoclaw" por
  tempo indefinido, por decisão explícita do R4.
- Cabeçalhos de copyright/licença preservados como "PicoClaw contributors"
  (atribuição MIT ao projeto upstream não é algo que um rebrand deva remover).
- README.md/ROADMAP.md/CONTRIBUTING.md e o exemplo `sipeed/picoclaw-skills` em
  `pkg/skills/install.go` foram deliberadamente deferidos para a consolidação
  de docs do E8 (evita reescrever a narrativa duas vezes).
