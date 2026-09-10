---
id: S08
title: Regras de negócio (invariantes)
status: confirmed
version: 1
owner: André
last_updated: 2026-09-10
depends_on: [S06]
---

# S08 — Regras de negócio (invariantes do sistema)

Regras que nenhuma implementação pode violar. Transições/situações não listadas são proibidas.

| ID | Regra | Escopo | Comportamento em violação |
|---|---|---|---|
| BR-001 | O modo dormir nunca executa enquanto houver turn de agente ativo | `pkg/sleep` | A rodada é pulada e re-agendada; log de aviso |
| BR-002 | O candidato nativo só entra na fallback chain se o binário foi compilado com `nativellm` **e** o GGUF existe em disco | `pkg/config` (ApplyNativeFallback) | Candidato não é adicionado; nenhum erro para o usuário |
| BR-003 | Conteúdo pessoal (hub, sessões, `.security.yml`, `docker/data/`, GGUFs) jamais entra no git nem no binário embutido | repo inteiro | Bloqueia commit/push; corrigir `.gitignore`/embed antes de prosseguir |
| BR-004 | O build padrão (`make build`) permanece `CGO_ENABLED=0`, Go puro, sem dependência do submodule | Makefile / CI | Build quebrado = regressão; reverter |
| BR-005 | Modo dormir é **desligado por padrão**; só ativa com `sleep.enabled: true` explícito no config | `pkg/config` | Config ausente ⇒ nenhuma goroutine de sono é criada |
| BR-006 | O modo dormir respeita `sleep.max_tokens_budget` como teto rígido de tokens LLM por rodada | `pkg/sleep` | Pipeline interrompe no meio e grava relatório parcial |
| BR-007 | Ações destrutivas do `sysmon` (kill/renice) são negadas por padrão; exigem habilitação explícita no config | `pkg/tools` | Tool retorna erro "ação desabilitada" |
