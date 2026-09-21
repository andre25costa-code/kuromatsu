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
(ligado/desligado) /`Memory` (`default`/`core`/`off`) — `core` só manda o
`MEMORY.md`, `off` não manda nada, útil pra não vazar memória pessoal pra
um provider externo turbinado (ver `models.md`, Ollama Cloud).

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
diagnóstico do que aconteceu (ver `docs/internal-audit-2026-09-16.md`,
seção "Mensagem espontânea no Telegram"). Confirmado ao vivo em 2026-09-21
(ver `BACKLOG.md`, "Incidente ao vivo") que esse é exatamente o mecanismo
por trás de um caso real: um job com `payload.message` em vez de
`payload.command` acabou custando 7 cron jobs duplicados, porque
`cron add` não é idempotente por `job_id` e o modelo, seguindo uma
instrução de `HEARTBEAT.md` que não fazia sentido nesse servidor, insistiu
em "agendar" a mesma coisa turno após turno. Prefira sempre
`payload.command` para qualquer notificação/checagem que não precise de
raciocínio — não só é mais barato, é a diferença entre "um erro repetido"
e "N efeitos colaterais reais repetidos".

## Reflexos (`agents.defaults.reflexes`)

Regex → ação determinística (`reply` com template, `command` — roda um
slash command, ou `exec`) **antes** do roteador de foco, com custo zero de
token. Só pra origem `user`. `Persist` controla se a troca entra na
sessão (default: sim pra `reply`/`command`, não pra `exec`).
