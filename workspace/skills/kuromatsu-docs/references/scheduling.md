# Cron, heartbeat e janelas de foco

Fonte real: `pkg/tools/cron.go`, `pkg/cron/service.go`,
`pkg/heartbeat/service.go`, `pkg/agent/focus.go`, `pkg/config/focus.go`,
`pkg/routing/focus.go` (decisão de projeto: o roteamento de foco e os
reflexos são determinísticos e não gastam token de LLM).

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
`memory-management.md`.

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
diagnóstico do que aconteceu e chegando ao chat como uma mensagem
espontânea. Um turno de LLM também pode repetir ações: um modelo seguindo
uma instrução do `HEARTBEAT.md` que não fazia sentido já "agendou" a mesma
coisa turno após turno. Hoje um `add` idêntico (mesmo nome, agenda,
mensagem e destino) devolve o job existente em vez de duplicar, e um `add`
idêntico com outro comando falha sem mudar nada; mesmo assim, prefira
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
job roda, ou com o gateway parado, se perde).

### Riscos residuais do cron

- **Lost update entre CLI e gateway:** não há trava entre processos. O
  gateway relê o `jobs.json` antes de cada alteração, mas uma gravação da
  CLI no mesmo instante de uma gravação do gateway pode se perder (a
  última grava por cima). Depois de editar pela CLI com o gateway
  rodando, confira com `kuromatsu cron list`.
- **Rename no Windows:** o `jobs.json` é gravado de forma atômica
  (arquivo temporário + rename). No Windows o rename pode falhar se outro
  processo estiver com o arquivo aberto (antivírus, indexador, um editor).
  A falha é registrada (`Failed to write cron store`) e quem pediu a
  alteração recebe erro; o processo que falhou continua com o estado novo
  em memória (o do outro processo pode diferir) e a próxima alteração grava
  o arquivo inteiro de novo.
- **Auto-desligamento:** um job de alta frequência que falha por mais de
  1 h seguida (e pelo menos `max_consecutive_failures` vezes) é desligado
  e fica desligado até alguém religar (ver
  [automations.md](automations.md#reabilitar-um-job-auto-desligado)).

### Criar e gerir automações

As formas de criar uma automação, o catálogo de modelos, os comandos
`/cron` e `/config`, a regra de dono, a resposta silenciosa `NADA` e como
religar um job auto-desligado estão em [automations.md](automations.md).

## Reflexos (`agents.defaults.reflexes`)

Regex → ação determinística (`reply` com template, `command` — roda um
slash command, ou `exec`) **antes** do roteador de foco, com custo zero de
token. Só pra origem `user`. `Persist` controla se a troca entra na
sessão (default: sim pra `reply`/`command`, não pra `exec`).
