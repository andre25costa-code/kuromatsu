# Tools do agente

Fonte real: `pkg/tools/`, `pkg/seahorse/` (memória opcional em SQLite/FTS5).

## Catálogo (janela `full`, todas as tools do registro global)

`append_file`, `edit_file`, `exec`, `find_skills`, `install_skill`,
`list_dir`, `load_image`, `message`, `reaction`, `read_file`, `send_file`,
`send_tts`, `spawn`, `subagent`, `sysmon`, `web_fetch`, `web_search`,
`write_file`, `cron`, `delegate` (auto-registrada só quando há mais de um
agente configurado). Com o backend seahorse habilitado, mais duas: `grep`
e `expand` (busca/expansão de memória de conversa).

Cada agente só vê o subconjunto que a janela de foco ativa permite —
ver `scheduling.md`. Uma tool que existe no registro global mas está fora
da janela atual não é "não encontrada": o modelo recebe uma dica de nomes
parecidos e, se insistir, o turno escala pra uma janela maior.

## Isolamento entre agentes/sessões

Duas tools guardam estado por sessão e checam a que sessão/agente ele
pertence:

- **`exec` (sessões de shell em background)**: cada `ProcessSession` agora
  tem um `owner` derivado da identidade real do runtime (workspace + agent
  ID + session key + channel + chat ID —
  `pkg/tools/shell.go:processOwner`). `list` só mostra sessões do próprio
  chamador; `poll`/`read`/`write`/`kill` (qualquer ação que não seja
  `run`/`list`) rejeitam agir sobre uma sessão de outro dono, sem
  distinguir "não autorizado" de "não existe" na mensagem de erro. `exec`
  em si é restrito a canais internos a menos que `allow_remote` esteja
  explicitamente ligado.
- **`grep`/`expand` (seahorse)**: o escopo de conversa vem sempre do
  contexto de sessão real (`pkg/seahorse/tool_scope.go`,
  `toolConversation(ctx)`), nunca de um argumento que o modelo passa.
  `all_conversations: true` é recusado — não existe caminho pra um modelo
  pedir memória de outra conversa. `expand` autoriza cada `messageID`
  contra a conversa atual antes de devolver qualquer conteúdo.

Regra geral pra qualquer tool nova que guarde estado por sessão: a
identidade vem do contexto injetado pelo runtime (`ToolSessionKey`,
`ToolAgentID`, `ToolChannel`, `ToolChatID` em `pkg/tools/`), nunca de um
argumento da própria chamada de tool.

## Schema compacto (`tool_schema_transform`)

`pkg/providers/common/compact_schema.go` reduz descrição a uma frase e
limita propriedades/enum aninhados — pensado pra modelos pequenos/locais
onde overhead de framework é caro. Todas as propriedades continuam
presentes (só o `type` sobrevive em aninhados), então o modelo ainda sabe
que o parâmetro existe.

## Log de execução de tool

`pkg/tools/registry.go` loga metadados de execução (nome, contagem de
argumentos, duração) — **não** loga o conteúdo dos argumentos nem o texto
de erro completo (isso poderia vazar segredo/PII pro log, que é menos
protegido que a resposta ao chamador).
O detalhe do erro continua disponível na resposta normal da tool.

## `exec` — o essencial

- **Onde roda:** `tools.exec.allow_remote` (default `false`) deixa o exec
  só nos canais internos (`cli`, `system`, `subagent`); num canal externo
  (Telegram, WhatsApp, Pico) ele é recusado até o dono ligar a chave.
- **Timeout:** `tools.exec.timeout_seconds` (default 60; `0` = sem teto). O
  argumento `timeout` (segundos) de cada chamada pode pedir menos; um valor
  maior é cortado no configurado, e `0`/ausente usa o configurado.
- **Saída presa:** depois que o comando termina, um filho em background que
  segura os pipes de saída só é esperado por 2 s (`WaitDelay`); a chamada
  volta com o que já saiu.
- **Sessões em background** (`background=true`, retorna `sessionId`): no
  máximo 8 rodando ao mesmo tempo no processo; uma sessão rodando há mais de
  1 h é morta pela limpeza periódica; uma sessão terminada fica legível por
  30 min e depois some. `poll`/`read`/`write`/`kill` operam sobre ela —
  sempre sujeitas à checagem de dono acima. Saída: até 10.000 bytes numa
  execução em primeiro plano, buffer de 1 MB numa sessão em background.
- **Guarda de comandos:** padrões de negação (`enable_deny_patterns`,
  ligado por padrão, mais `custom_deny_patterns`), allowlist opcional
  (`custom_allow_patterns`) e, com `agents.defaults.restrict_to_workspace`,
  bloqueio de `..` como caminho e de caminhos absolutos fora do workspace.

### Riscos residuais da guarda do exec

A guarda é de **melhor esforço**, por padrão de texto, e **não** é um
sandbox. Para isolamento real, ver [isolation.md](isolation.md). Com
`restrict_to_workspace` ligado, no Unix:

- `cd ~`, `~/...`, `$HOME` e `$HOME/...` não são expandidos antes da
  checagem (a expansão de `~` e de `$env:` só existe no Windows), então
  passam e alcançam fora do workspace.
- Symlinks só são resolvidos em caminhos que a guarda reconhece (um trecho
  com `/`, como `link/arquivo`). Um symlink do workspace usado pelo nome
  sozinho (`cat link`, `ls link`, `cd link && ...`) passa, e um symlink
  criado na hora (`ln -s ~ h && ls h`) também.
- `$(...)` e `${...}` são recusados pelos padrões de negação padrão; com
  `enable_deny_patterns: false` deixam de ser.

Mitigação: `tools.exec.allow_remote` desligado (o padrão) mantém o exec
fora dos canais externos; `restrict_to_workspace`; isolamento de processos
filhos (`bwrap`, ver [isolation.md](isolation.md)).

## `web` — busca e fetch

`web_search` usa os provedores ligados em `tools.web.*`; o DuckDuckGo
(`tools.web.duckduckgo.enabled`) vem ligado por padrão e não precisa de
chave. `/config set tools.web.duckduckgo.enabled off` desliga pelo chat (ver
[automations.md](automations.md)). `web_fetch` baixa uma página
(`tools.web_fetch.enabled`).

## Memória de conversa (seahorse, opcional)

Backend SQLite/FTS5 compartilhado — complementa (não substitui) as sessões
JSONL normais. `grep` busca por padrão/regex com filtro de papel/tempo;
`expand` recupera o texto completo de mensagens específicas por ID. Os dois
sempre com o escopo derivado da sessão real, nunca de argumento (ver
acima).
