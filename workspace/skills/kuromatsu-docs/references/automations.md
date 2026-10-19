# Automações: criar e gerir

Fonte real: `pkg/automation/` (catálogo em `pkg/automation/templates.json`),
`pkg/commands/cmd_cron.go`, `pkg/commands/cmd_config.go`,
`pkg/setup/owner.go`, `pkg/setup/configkeys.go`, `pkg/tools/cron.go`,
`cmd/kuromatsu/internal/cron/`.

Esta página é o "como fazer". O mecanismo (serviço de cron, `jobs.json`,
`runs.json`, status, auto-desligamento, `tz`, CLI com o gateway rodando,
heartbeat) está em [scheduling.md](scheduling.md).

## Quatro formas de criar uma automação

1. **Linguagem natural:** pedir ao agente ("me lembra de beber água todo
   dia às 15h"); o modelo chama a tool `cron`. Depende da qualidade do
   modelo; confira o resultado com `/cron list`.
2. **Chat:** `/cron new <modelo> chave=valor ...` com um modelo do catálogo
   (abaixo). O job é entregue no chat que o criou.
3. **Web UI:** tela Automações (ver [webui.md](webui.md)). O destino
   padrão é o primeiro canal ligado que tem um ID em `allow_from`.
4. **CLI:** `kuromatsu cron add` (`--cron` + `--tz`, `--every <segundos>`,
   `--message` para um turno do agente ou `--command` para um comando sem
   LLM, `--channel`/`--to` para o destino, `--quiet`/`--anomaly-regex`/
   `--on-anomaly` para avisar só em anomalia). Para tarefas que pedem
   julgamento a cada execução, prefira o `HEARTBEAT.md` (ver
   [scheduling.md](scheduling.md)).

Um `add` idêntico a um job existente (mesmo nome, agenda, mensagem e
destino) devolve o job existente em vez de duplicar.

## Catálogo de modelos

| id | Parâmetros | Agenda | Requer |
|---|---|---|---|
| `lembrete` | `texto`; `quando` (RFC3339 ou `HH:MM`, hoje/amanhã) **ou** `todo_dia` (`HH:MM`) | `at` (apagado após rodar) ou diário | `tools.cron.enabled` |
| `resumo-diario` | `hora` (padrão `08:00`) | diário | `tools.cron.enabled` |
| `monitorar-pagina` | `url` (http/https), `condicao`, `a_cada_horas` (1-24, padrão 6) | a cada N horas, janela `web`, responde `NADA` quando a condição é falsa | `tools.cron.enabled`, `tools.web.enabled`, `tools.web_fetch.enabled` |
| `pesquisa-diaria` | `tema`, `hora` (padrão `07:30`) | diário, janela `web` | `tools.cron.enabled`, `tools.web.enabled` |

Os horários `HH:MM` e as agendas diárias usam o `timezone` do config
(vazio = fuso do processo, gravado como `tz` vazio). Não há modelo de
e-mail (não existe tool de e-mail). `resumo-diario` e `pesquisa-diaria`
dependem da qualidade do modelo em uso: com o modelo nativo pequeno
(Bonsai de 1,7B) podem sair rasos; prefira um modelo externo para eles.
Um modelo cujos requisitos (`Requer`) estão desligados é recusado.

## Comandos `/cron`

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

## `/config` e `/config set`

`/config` mostra um resumo sem segredos (modelo, canais com contagem de
IDs, preset de permissões, heartbeat, fuso, busca web, limite do turno,
nível de log). `/config set <chave> <valor>` aceita só `heartbeat.enabled`,
`heartbeat.interval` (≥ 5), `agents.defaults.user_turn_max_minutes` (≥ 0),
`tools.web.duckduckgo.enabled`, `gateway.log_level`
(`debug|info|warn|error`) e `timezone` (nome IANA ou `""`; o relógio do
processo só muda após reiniciar). A gravação lê o arquivo sem variáveis de
ambiente, valida sem mexer no estado do processo, salva e recarrega o
gateway.

Chaves de segurança (`allow_from`, exec, restrição ao workspace, chaves de
API) nunca mudam pelo chat: use a web UI ou a CLI.

## Quem pode alterar pelo chat (regra de dono)

Ler (`/cron list|history|templates`) vale para qualquer remetente aceito
pelo canal. Alterar (`/cron pause|resume|run|new`, `/config set`) exige ser
o dono: canal interno (`cli`, `system`, `subagent`), ou o **ID** do
remetente listado no `allow_from` do canal como `123`, `telegram:123` ou
`123|nome`. Username (`@alice`, `alice`) e `*` não dão esse direito. No
WhatsApp o ID é o JID que chega como remetente (`5511999999999@s.whatsapp.net`,
às vezes com `:<dispositivo>`): liste exatamente esse valor ou
`whatsapp:<jid>`. Num bot aberto (`["*"]`) sem o ID do dono, ninguém altera
pelo chat, e o resumo do `/config` também fica restrito ao dono. Por isso,
num bot aberto, liste também o seu ID: `["*", "123"]`.

## Resposta silenciosa `NADA`

Se a resposta final do turno de um job de mensagem for exatamente `NADA`
(sem diferenciar maiúsculas, ignorando espaços e pontos nas pontas), nada é
enviado ao chat e a execução conta como `ok` no histórico. Serve para
verificações que só devem falar quando há algo a relatar ("responda NADA
se não mudou"). Só a palavra sozinha é suprimida: "NADA de novo" é
enviada. Um job antigo cuja resposta legítima seja exatamente "NADA" deixa
de ser entregue.

## Reabilitar um job auto-desligado

Depois de `tools.cron.max_consecutive_failures` falhas seguidas (default
5), com pelo menos 1 h desde a primeira, o job é desligado e o destino
recebe um aviso (regra completa em [scheduling.md](scheduling.md)). Para
religar, corrija a causa (`/cron history <id>` ou `kuromatsu cron history
<id>` mostra o erro) e use um destes, que zeram a sequência de falhas:

- `/cron resume <id>` no chat;
- Retomar na tela Automações;
- `kuromatsu cron enable <id>`, ou `kuromatsu cron edit <id>` mudando a
  agenda ou o conteúdo.

## Proteções de loop em turnos de automação

Turnos de cron e heartbeat não usam o teto do turno de usuário: usam
`agents.defaults.non_user_turn_max_minutes` (default 60; `0` desliga). No
estouro a execução conta como falha. A guarda de repetição
(`max_repeated_tool_calls`) vale para todos os turnos. As chaves estão em
[config.md](config.md#guardas-do-loop-do-agente).
