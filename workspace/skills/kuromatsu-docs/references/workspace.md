# Workspace do agente

O workspace (padrão `~/.kuromatsu/workspace`, ou `agents.defaults.workspace`)
define quem o agente é e o que ele pode fazer — só com arquivos de texto, sem
código. `kuromatsu onboard` cria os modelos; rodar de novo **não** sobrescreve
arquivos existentes (`--force` restaura os modelos).

| Arquivo | Papel |
|---|---|
| `AGENT.md` | Papel e instruções do agente (corpo) + configuração no frontmatter |
| `SOUL.md` | Personalidade, valores e estilo de comunicação |
| `USER.md` | Quem é o usuário: preferências, contexto, fuso |
| `HEARTBEAT.md` | Tarefas periódicas — ver [scheduling.md](scheduling.md) |
| `memory/MEMORY.md` | Memória de longo prazo — ver [sleep.md](sleep.md) e [tools.md](tools.md) |
| `skills/<nome>/SKILL.md` | Skills do workspace (abaixo) |

`AGENTS.md` e `IDENTITY.md` são o formato antigo: só são lidos quando
`AGENT.md` não existe.

## Frontmatter do `AGENT.md`

```markdown
---
name: kuro
description: Assistente pessoal
model: gemma-cloud          # opcional
tools: [read_file, web_search, message]   # opcional
maxTurns: 8                               # opcional
skills: [weather, summarize]              # opcional
mcpServers: [github]                      # opcional
---
Você é ... (instruções do agente)
```

| Campo | Efeito (código: `pkg/agent/instance.go`, `tool_allowlist.go`) |
|---|---|
| `model` | Modelo principal deste agente. Vence `agents.list[].model.primary` e `agents.defaults.model_name`. Deve ser um `model_name` do `model_list`. |
| `tools` | Lista de tools permitidas. **Campo ausente** = todas; **lista vazia** = nenhuma. Nomes desconhecidos geram aviso no log. |
| `skills` | Filtro de skills visíveis ao agente. Ausente = todas; vence `agents.list[].skills`. |
| `mcpServers` | Servidores MCP permitidos (nomes de `tools.mcp.servers`). Ausente = todos; lista vazia = nenhum. |
| `maxTurns` | Máximo de iterações de tools por turno deste agente; vence `agents.defaults.max_tool_iterations`. Ausente ou ≤ 0 = o padrão. |

Frontmatter com YAML inválido falha fechado: o agente fica **sem tools e sem
MCP** até o arquivo ser corrigido (o erro aparece no log).

## Skills

Uma skill é uma pasta com `SKILL.md`; o frontmatter precisa de `name` e
`description` (a descrição é o que o agente vê para decidir usá-la):

```markdown
---
name: weather
description: Previsão do tempo sem chave de API.
---
Instruções da skill...
```

Ordem de busca (o primeiro nome encontrado vence): `workspace/skills` →
`~/.kuromatsu/skills` (global) → o diretório de `KUROMATSU_BUILTIN_SKILLS`,
se definido.

As skills embutidas no binário são copiadas para o workspace por `onboard` e
por `kuromatsu skills install-builtin` (só as que faltam; uma skill existente,
mesmo editada, não é tocada). O `onboard` pula as skills cujas ferramentas
(`metadata.nanobot.requires.bins`) não estão no PATH ou cujo `os` não inclui
este sistema, e lista o que faltou; `install-builtin` instala todas. `kuromatsu skills list-builtin` mostra quais
existem e quais já estão instaladas.
