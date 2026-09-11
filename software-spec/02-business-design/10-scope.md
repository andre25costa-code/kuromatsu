---
id: S10
title: Escopo — o que fica, o que sai, o que não entra
status: confirmed
version: 2
owner: André
last_updated: 2026-09-11
depends_on: [S01]
---

# S10 — Escopo

## Dentro do escopo (fica / é criado)

| Área | Conteúdo |
|---|---|
| Core do agente | Loop, pipeline, fallback chain, tools, sessões, seahorse, cron, heartbeat, evolution — herdados do PicoClaw |
| Canais | `pico`, `pico_client`, `telegram`, `whatsapp`, `whatsapp_native` |
| **Novo** | Provider nativo in-process (`pkg/providers/localllm`), fallback automático sem chaves, modo dormir (`pkg/sleep`), tool `sysmon`, skill `kuromatsu-docs`, build/deploy nativo x86_64 (Oracle) |
| Workspace | Template genérico PT-BR versionado + embed para `onboard` |

## Fora do escopo (removido nesta refatoração — ADR-007)

| Removido | Motivo |
|---|---|
| `web/` (launcher Go+React, ~31k linhas) | Usuário opera via CLI/Docker/chat; segundo binário sem uso |
| ~18 canais (`feishu`, `weixin`, `wecom`, `dingtalk`, `slack*`, `matrix`, `deltachat`, `line`, `onebot`, `qq`, `irc`, `vk`, `maixcam`, `teams_webhook`, `mqtt`) | Não usados; reduzem manutenção, deps e superfície de rebrand |
| Tools de hardware (`i2c`, `spi`, `serial`) | Voltados a boards Sipeed; substituídos pelo `sysmon` (FR-011) |
| `docs/` legado (213 arquivos, 10 idiomas), `examples/`, suítes de integração órfãs | Documentação renasce consolidada na skill `kuromatsu-docs` (ADR-010) |

## Explicitamente adiado (não é "não", é "depois")

| Item | Condição de retomada |
|---|---|
| Streaming do provider nativo (`ChatStream`) | Gancho `onToken` já fica no loop do engine (E5); implementar quando houver canal que aproveite |
| Grammar sampling p/ tool-calls (`llama_sampler_init_grammar`) | Se o parser tolerante (FR-002) se mostrar insuficiente na prática |
| Banco vetorial próprio em Go | Só se FTS5/BM25 (ADR-008) deixar de atender o recall necessário |
| Suporte a outros GGUFs no provider nativo (4B/8B) | Após baseline do 1.7B na máquina de 1 GB |

## Fica privado (nunca entra no repo)

Hub pessoal de estudos (`hub_core` Python, dados, sessões) — permanece em `docker/data/workspace/`, gitignorado (BR-003).
