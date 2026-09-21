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

## Isolamento entre agentes/sessões (achado de segurança, corrigido 2026-09-16)

Duas tools guardavam estado global sem checar a que sessão/agente ele
pertencia — corrigido no audit (`docs/internal-audit-2026-09-16.md`,
SEC-01/SEC-02):

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
de erro completo (achado SEC-03 do audit: isso vazava potencialmente
segredo/PII pro log, que é menos protegido que a resposta ao chamador).
O detalhe do erro continua disponível na resposta normal da tool.

## `exec` — o essencial

Timeout configurável (`tools.exec.timeout_seconds`, default 60s), padrões
de deny/allow customizáveis, `restrict_to_workspace`. Sessão em background
(`action: run` com processo longo) sobrevive entre chamadas via
`sessionId`; `poll`/`read`/`write`/`kill` operam sobre ela — sempre sujeitas
à checagem de dono acima.

## Memória de conversa (seahorse, opcional)

Backend SQLite/FTS5 compartilhado — complementa (não substitui) as sessões
JSONL normais. `grep` busca por padrão/regex com filtro de papel/tempo;
`expand` recupera o texto completo de mensagens específicas por ID. Os dois
sempre com o escopo derivado da sessão real, nunca de argumento (ver
acima).
