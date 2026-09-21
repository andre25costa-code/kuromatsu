---
name: kuromatsu-docs
description: Documentação canônica do Kuromatsu (config, modelos, canais, tools, cron/heartbeat, sono, RAM, deploy) — árvore de decisão por tópico. Use ao configurar/depurar o Kuromatsu, ao explicar como um subsistema funciona, ou antes de escrever documentação nova sobre ele (evita duplicar o que já existe em outro arquivo). ADR-010.
---

# Documentação do Kuromatsu

Fonte canônica única (ADR-010): um arquivo por tópico, sem duplicação. Se a
pergunta cabe num dos tópicos abaixo, a resposta já existe em `references/` —
leia o arquivo certo antes de explicar de memória ou de criar documentação
nova sobre o mesmo assunto.

## Árvore de decisão

| Pergunta sobre... | Arquivo |
|---|---|
| `config.json`, `.security.yml`, variáveis `KUROMATSU_*`, onde um campo é lido | [references/config.md](references/config.md) |
| `model_list`, providers, fallback entre modelos, o modelo nativo Bonsai | [references/models.md](references/models.md) |
| Telegram, WhatsApp, Pico, como adicionar um canal novo | [references/channels.md](references/channels.md) |
| Tools do agente (exec, arquivos, memória/seahorse, web, skills) | [references/tools.md](references/tools.md) |
| cron, heartbeat, janelas de foco (`/foco`), reflexos | [references/scheduling.md](references/scheduling.md) |
| Modo dormir, consolidação de `MEMORY.md`, `pkg/sleep` | [references/sleep.md](references/sleep.md) |
| Orçamento de RAM, `memguard`, PSI, `GOMEMLIMIT`, motor `runstate` | [references/memory-management.md](references/memory-management.md) |
| Deploy em produção (`demetrius`), systemd, backup, CI | [references/deploy.md](references/deploy.md) |
| Voz — transcrição (ASR) e síntese de fala (TTS) | [references/voice.md](references/voice.md) |
| Isolamento de processos filhos (`bwrap`, tokens restritos) | [references/isolation.md](references/isolation.md) |
| Como rodar/adicionar testes, suítes de integração | [references/testing.md](references/testing.md) |

Nenhum tópico acima? Não crie um `README.md` novo em pasta nenhuma — ou o
assunto cabe num arquivo existente (edite-o), ou é um tópico novo de verdade
(peça para o André confirmar, então crie `references/<topico>.md` e adicione
uma linha nesta tabela). Regra de dono único (唯一归属): cada fato mora em
exatamente um arquivo canônico; qualquer outra menção é um link para ele, não
uma cópia.

## O que NÃO está aqui

- Decisão arquitetural e histórico de trade-offs → `software-spec/07-engineering/adr/`.
- Requisitos e critérios de aceite (FR/AC) → `software-spec/02-business-design/11-requirements.md`.
- Convenções de código/commit para quem desenvolve o Kuromatsu com Claude Code → `AGENTS.md` (raiz do repo).

Esta skill é o manual operacional — como o sistema se comporta e como
configurá-lo — não o porquê da decisão nem o contrato de aceite.
