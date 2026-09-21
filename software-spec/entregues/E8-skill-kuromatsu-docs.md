---
id: E8
title: Skill kuromatsu-docs — documentação canônica em PT-BR, um arquivo por tópico
status: done
frs: [FR-009]
adrs: [ADR-010]
commits: [c2c1c765]
last_updated: 2026-09-21
---

# E8 — Skill `kuromatsu-docs`

## O que foi entregue

`workspace/skills/kuromatsu-docs/` — skill canônica em PT-BR (ADR-010),
com `SKILL.md` navegando por árvore de decisão para 11 arquivos de
referência em `references/`: `config.md`, `models.md`, `channels.md`,
`tools.md`, `scheduling.md`, `sleep.md`, `memory-management.md`,
`deploy.md` (os 8 tópicos que ADR-010 lista), mais `voice.md`,
`isolation.md` e `testing.md` (conteúdo real que já existia disperso em
READMEs por pasta e precisava de uma casa). `.claude/skills/kuromatsu-docs/`
criado como shim apontando para a canônica (ADR-010, ponto 3).

Migração de conteúdo, não cópia: os 9 READMEs por pasta (`integration/`,
`pkg/audio/asr(+.zh)`, `pkg/audio/tts(+.zh)`, `pkg/channels(+.zh)`,
`pkg/isolation(+.zh)`) foram lidos, o conteúdo tecnicamente correto foi
migrado e traduzido pra PT-BR, e o que estava desatualizado foi corrigido
na migração — notavelmente `pkg/channels/README.md` (1434 linhas) listava
canais que não existem mais (Discord, Slack, LINE, OneBot, DingTalk,
Feishu, WeCom, QQ, Matrix, MQTT) e um guia de migração de uma refatoração
antiga já concluída; `references/channels.md` documenta só os 4 canais
reais (`telegram`, `whatsapp`, `whatsapp_native`, `pico`), verificados
contra o código (`pkg/channels/manager.go`'s `channelRateConfig` real, e
os métodos de interface opcional que cada canal de fato implementa).

## FRs cobertas

| FR | Descrição | ACs | Status |
|---|---|---|---|
| FR-009 | Skill `kuromatsu-docs` | AC-009-1 | Verificado — skill descoberta pelo próprio harness (apareceu na lista de skills disponíveis assim que criada) e pelo carregador real (`pkg/skills/loader.go`, `os.ReadDir` em `workspace/skills/`, mesmo padrão das skills já existentes ali) |
| | | AC-009-2 | Verificado — 8 tópicos exigidos pela ADR-010 (config, modelos, canais, tools, cron/heartbeat, sono, RAM, deploy) têm cada um exatamente 1 arquivo canônico em `references/`, mais 3 tópicos extra (voice, isolation, testing) que precisavam de destino depois da migração dos READMEs |
| | | AC-009-3 | Verificado — `git ls-files '*README*'` reduz a só `README.md` raiz (9 arquivos removidos, decisão confirmada explicitamente pelo André antes da remoção) |

## Commits

| Commit | Mensagem |
|---|---|
| `c2c1c765` | feat(docs,config,agent): kuromatsu-docs skill (E8), S36-38, per-agent sleep (ADR-019) |

Commit único, compartilhado com S36/S37/S38 e a config de sono por agente
(ver `BACKLOG.md` para o registro completo desta rodada, incluindo o
incidente ao vivo encontrado durante a investigação).

## Arquivos-chave

`workspace/skills/kuromatsu-docs/{SKILL.md,references/*.md}` (11 arquivos
de referência), `.claude/skills/kuromatsu-docs/SKILL.md` (shim),
`CONTRIBUTING.md` (link pra `integration/README.md` corrigido pra apontar
pra `references/testing.md`).

## Como verificar

```bash
git ls-files '*README*'                          # só README.md raiz
find workspace/skills/kuromatsu-docs -type f      # SKILL.md + 11 references/
```

Descoberta real pelo agente: subir o Kuromatsu com esse workspace e checar
que `find_skills` (ou a lista de skills carregadas no boot) inclui
`kuromatsu-docs`.

## O que ficou fora, deliberadamente

- **`README.md` raiz não foi encurtado.** ADR-010 (ponto 4) sugere que ele
  fique curto (identidade + quickstart), mas isso não é uma AC de FR-009 —
  é uma consequência da decisão, não o contrato de aceite. O README raiz
  de hoje (673 linhas) ainda descreve features do upstream PicoClaw que
  este fork não necessariamente mantém do mesmo jeito (WebUI Launcher,
  app Android, rede social ClawdChat) — decisão de identidade/branding que
  é do André, não algo para eu decidir sozinho ao "implementar uma spec
  em falta". Fica como follow-up explícito, não escondido.
- **PT-BR, não bilíngue.** As versões `.zh` dos READMEs antigos não foram
  recriadas — ADR-010 pede "uma fonte canônica em PT-BR", não um par
  PT-BR/中文 por tópico. Se o André quiser tradução chinesa de volta, é
  uma decisão nova, não uma correção de um item que "ficou em falta".
