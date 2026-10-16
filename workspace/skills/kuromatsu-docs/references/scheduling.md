# Cron, heartbeat e janelas de foco

Fonte real: `pkg/tools/cron.go`, `pkg/cron/service.go`,
`pkg/heartbeat/service.go`, `pkg/agent/focus.go`, `pkg/config/focus.go`,
`pkg/routing/focus.go`, ADR-014.

## Janelas de foco (`agents.defaults.focus`)

O roteador de foco decide, **sem gastar token**, qual conjunto de
tools/skills/histórico/modo de prompt usar num turno — regex/keyword/
origem, não LLM. Com `focus.enabled: false` (default), o comportamento é
byte-idêntico ao perfil estático de sempre; ninguém precisa pensar em
janela.

Precedência de decisão (`pkg/routing/focus.go`, `FocusRouter.Route`):
explícito (comando `/foco <janela>`) → tag inline `[foco:janela]` no
começo da mensagem → origem não-usuária (`heartbeat`/`cron`/`system` mapeiam
pra janela própria) → regra configurada (keyword, depois regex, na ordem
da config) → aderência de sessão (`/foco` sem mensagem fixa a janela pelas
próximas N mensagens) → default.

Janelas padrão (`config.DefaultFocusWindows()`): `chat` (sem tools),
`files`, `shell`, `web`, `schedule`, `memory`, `full` (teto do
escalonamento, = perfil sem restrição), `heartbeat`, `cron`. Cada uma
define `Tools`/`Skills`/`SystemPrompt` (modo `compact` ou não) /`History`
(ligado/desligado) /`Memory` (`default`/`core`/`off`/`retrieved`) — `core`
só manda o `MEMORY.md`, `off` não manda nada, útil pra não vazar memória
pessoal pra um provider externo turbinado (ver `models.md`, Ollama Cloud).
`retrieved` (opt-in) importa o `MEMORY.md` para `memory/atoms.db` e manda só
o que cabe em `memory_budget_tokens`: regras, preferências e itens fixados no
prompt de sistema, e os fatos que casam com a mensagem dentro da própria
mensagem daquela chamada. Com o banco vazio age como `core`. Use com provider
externo: no modelo nativo esse bloco por turno é reprocessado a ~1 token/s.

**Escalonamento**: se o modelo chama uma tool que existe no registro global
mas está fora da janela atual, o turno escala pra outra janela
(`EscalateTo`, no máx. 1× por turno — `MaxPerTurn`) e refaz só o prompt de
sistema (o prefixo do KV cache, se `core_cache_parking` estiver ligado,
sobrevive à troca — ver `memory-management.md`). Se a tool nem existe no
registro global, é erro imediato com dica de nome parecido, sem escalar
nada.

## Heartbeat (`agents.defaults.heartbeat`, ou por agente)

Um `heartbeat.HeartbeatService` por agente com heartbeat habilitado
(`agents.list[].heartbeat`, sobrescreve campo a campo o bloco global —
`Config.EffectiveHeartbeat(agentID)`). `agents.list` vazio = exatamente um
serviço, no agente implícito `main`, idêntico a antes do multi-agente.
`SessionKey` do turno de heartbeat é `"heartbeat:" + agentID` (não
`"heartbeat"` fixo — evita heartbeats de agentes diferentes se
suprimirem mutuamente no controle de turnos concorrentes).

Com N agentes de heartbeat habilitados, o boot escalona o `Start()` de cada
um (2s de intervalo, `gateway.go`) em vez de disparar todos ~1s depois do
boot — evita fila real disputando o mesmo lock do `runstate.Engine` em
hardware modesto.

**`keep_alive_secs` tem que ser maior que o intervalo do heartbeat** (ou
`-1`, nunca descarrega) — senão o modelo nativo recarrega do zero em todo
heartbeat, o oposto do que o cache de prefixo existe pra evitar. Ver
`memory-management.md`/S39 para o número real medido em produção.

## Cron (`tools.cron`)

Job cujo `payload.channel`/`.to` casa uma regra de `agents.dispatch.rules`
já roda no agente certo, pelo mesmo caminho que uma mensagem de canal usa
(`ProcessDirectWithChannel` → `resolveMessageRoute`) — nenhum código
dedicado, funciona hoje. `payload.agent_id` força um agente específico
independente de canal/chat (bypassa as regras de dispatch) quando o job
precisa disso.

Cada tarefa cron roda com `MaxLLMRetries`/`MaxToolIterations` normais do
agente. Se a tarefa é uma checagem determinística e simples (ex.: "roda
esse comando e me avisa se falhar"), preferir `payload.kind: command`
(execução direta, 0 tokens de LLM) a `payload.kind: message` (turno de LLM
completo) — mais barato e não tem o custo de latência do modelo local numa
tarefa que não precisa de raciocínio.

**Cuidado com erro técnico virando resposta ao usuário**: um job cujo
payload é uma mensagem de LLM, se uma tool dentro do turno falhar, o
próprio modelo formula a resposta final — inclusive podendo errar o
diagnóstico do que aconteceu (achado de um audit interno de 2026-09-16,
seção "Mensagem espontânea no Telegram"). Confirmado ao vivo em 2026-09-21
que esse é exatamente o mecanismo por trás de um caso real: um job com
`payload.message` em vez de `payload.command` acabou custando 7 cron jobs
duplicados, porque `cron add` não é idempotente por `job_id` e o modelo,
seguindo uma instrução de `HEARTBEAT.md` que não fazia sentido nesse
servidor, insistiu em "agendar" a mesma coisa turno após turno. Prefira sempre
`payload.command` para qualquer notificação/checagem que não precise de
raciocínio — não só é mais barato, é a diferença entre "um erro repetido"
e "N efeitos colaterais reais repetidos".

### Status, histórico e auto-desligamento

`state.lastStatus` é `ok`, `error` ou `canceled`, com a mensagem em
`state.lastError`. Conta como falha (`error`): exec desligado num job de
comando; comando sem tratamento de anomalia (`quiet`, `anomaly_regex`,
`on_anomaly`) que termina com erro; comando com tratamento de anomalia que
estoura o timeout do exec; turno de agente com erro (inclusive o teto
`non_user_turn_max_minutes`). Uma anomalia num job que a trata **não** é
falha: é o sinal previsto, entregue como antes. Um job interrompido pelo
desligamento ou reload do gateway fica `canceled`, que não conta nem como
falha nem como sucesso.

`tools.cron.max_consecutive_failures` (default 5; 0 = nunca;
env `KUROMATSU_TOOLS_CRON_MAX_CONSECUTIVE_FAILURES`) desliga um job depois
de N falhas seguidas, **desde que** já tenha passado pelo menos 1 h desde a
primeira delas, para que uma queda curta do provedor não desligue um job de
alta frequência. O `lastError` ganha o prefixo
`auto-disabled after N consecutive failures: ` e o destino do job
(`payload.channel`/`.to`, default `cli`/`direct`) recebe um aviso com o
comando para religar. `kuromatsu cron enable <id>` (ou `update` mudando
schedule/payload) zera a sequência.

As últimas 20 execuções de cada job ficam em `runs.json`, ao lado de
`jobs.json` (início, duração, status e erro truncado em 512 bytes). Só o
gateway grava esse arquivo; a CLI só lê. Um job `at` removido depois de
rodar perde o histórico junto. Um job `at` interrompido pelo desligamento
não é apagado: continua ativo e roda de novo 1 min depois (ou logo após o
reinício).

### Validação, fuso e CLI com o gateway rodando

`add` recusa expressão cron inválida, `at` no passado e fuso inexistente;
`update`/`edit` só validam quando o schedule muda, então um job antigo com
schedule vencido continua editável (mas `enable` de um `at` vencido falha).
`tz` (IANA, ex. `America/Sao_Paulo`) vale só para `cron_expr` (tool) /
`--cron` (CLI); sem ele, a expressão usa o fuso local do host. Mudar só a
expressão de um job cron mantém o fuso que ele já tinha.

A CLI (`kuromatsu cron list|add|edit|enable|disable|remove|run-now|history`)
pode alterar jobs com o gateway rodando: o gateway relê `jobs.json` quando
o conteúdo muda (no máximo a cada 1 min, e antes de cada alteração e de
cada leitura da tool), sem recalcular o próximo disparo. `run-now` só marca
o job como devido; quem o executa é o gateway (um `run-now` feito enquanto o
job roda, ou com o gateway parado, se perde). Não há trava entre
processos: uma gravação da CLI no mesmo instante de uma gravação do gateway
pode se perder.

### Resposta silenciosa `NADA`

Se a resposta final do turno de um job de mensagem for exatamente `NADA`
(sem diferenciar maiúsculas, ignorando espaços e pontos nas pontas), nada é
enviado ao chat e a execução conta como `ok` no histórico. Serve para
verificações que só devem falar quando há algo a relatar ("responda NADA
se não mudou"). Só a palavra sozinha é suprimida: "NADA de novo" é
enviada. Um job antigo cuja resposta legítima seja exatamente "NADA" deixa
de ser entregue.

### Comandos de chat `/cron` e catálogo de automações

Com o gateway rodando (em `kuromatsu agent` o comando responde
"Command unavailable in current context."):

- `/cron list`: id curto (8 caracteres), nome, agenda, estado
  (`on`/`paused`/`auto-disabled`) e próxima execução;
- `/cron pause <id>` / `/cron resume <id>` (o `resume` zera a sequência de
  falhas), `/cron run <id>` (marca como devido; resposta "queued"),
  `/cron history <id>` (últimas 5 execuções, erro cortado em 120
  caracteres);
- `/cron templates` e `/cron new <modelo> chave=valor ...` (valores com
  espaço entre aspas: `texto="beber água"`). O job é entregue no chat que
  o criou.

O `<id>` aceita um prefixo único de pelo menos 4 caracteres; um prefixo
ambíguo devolve erro com os candidatos. Um chat externo só vê e altera os
jobs entregues a ele (jobs de comando só se o chat estiver em
`tools.cron.command_allowed_remotes`); canais internos veem todos.

Modelos do catálogo (`pkg/automation/templates.json`):

| id | Parâmetros | Agenda | Requer |
|---|---|---|---|
| `lembrete` | `texto`; `quando` (RFC3339 ou `HH:MM`, hoje/amanhã) **ou** `todo_dia` (`HH:MM`) | `at` (apagado após rodar) ou diário | `tools.cron.enabled` |
| `resumo-diario` | `hora` (padrão `08:00`) | diário | `tools.cron.enabled` |
| `monitorar-pagina` | `url` (http/https), `condicao`, `a_cada_horas` (1-24, padrão 6) | a cada N horas, janela `web`, responde `NADA` quando a condição é falsa | `tools.cron.enabled`, `tools.web.enabled`, `tools.web_fetch.enabled` |
| `pesquisa-diaria` | `tema`, `hora` (padrão `07:30`) | diário, janela `web` | `tools.cron.enabled`, `tools.web.enabled` |

Os horários `HH:MM` e as agendas diárias usam o `timezone` do config
(vazio = fuso do processo, gravado como `tz` vazio). Não há modelo de
e-mail (não existe tool de e-mail). `resumo-diario` e `pesquisa-diaria`
dependem da qualidade do modelo em uso.

### Quem pode alterar pelo chat (`/cron`, `/config set`)

Ler (`/cron list|history|templates`) vale para qualquer remetente aceito
pelo canal. Alterar (`/cron pause|resume|run|new`, `/config set`) exige ser
o dono: canal interno (`cli`, `system`, `subagent`), ou o **ID** do
remetente listado no `allow_from` do canal como `123`, `telegram:123` ou
`123|nome`. Username (`@alice`, `alice`) e `*` não dão esse direito. No
WhatsApp o ID é o JID que chega como remetente (`5511999999999@s.whatsapp.net`,
às vezes com `:<dispositivo>`): liste exatamente esse valor ou
`whatsapp:<jid>`. Num bot aberto (`["*"]`) sem o ID do dono, ninguém altera
pelo chat, e o resumo do `/config` também fica restrito ao dono.

`/config` mostra um resumo sem segredos (modelo, canais com contagem de
IDs, preset de permissões, heartbeat, fuso, busca web, limite do turno,
nível de log). `/config set <chave> <valor>` aceita só `heartbeat.enabled`,
`heartbeat.interval` (≥ 5), `agents.defaults.user_turn_max_minutes` (≥ 0),
`tools.web.duckduckgo.enabled`, `gateway.log_level`
(`debug|info|warn|error`) e `timezone` (nome IANA ou `""`; o relógio do
processo só muda após reiniciar). A gravação lê o arquivo sem variáveis de
ambiente, valida sem mexer no estado do processo, salva e recarrega o
gateway.

## Reflexos (`agents.defaults.reflexes`)

Regex → ação determinística (`reply` com template, `command` — roda um
slash command, ou `exec`) **antes** do roteador de foco, com custo zero de
token. Só pra origem `user`. `Persist` controla se a troca entra na
sessão (default: sim pra `reply`/`command`, não pra `exec`).
