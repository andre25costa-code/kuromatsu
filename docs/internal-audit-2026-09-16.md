# Dossiê interno — Kuromatsu

Data: 2026-09-16. Base examinada: `1aa84c95`. Correções desta rodada ainda
não constituem um commit. Não contém credenciais ou conteúdo das conversas.

## Escopo e arquitetura

A exploração inicial foi somente leitura. Em seguida, o proprietário autorizou
testes, correções e atualização de produção via alias SSH `demetrius`.
Inventário inicial: 798 arquivos Go, dos quais 321 arquivos de teste. A revisão
foi dirigida aos caminhos principais e às fronteiras de segurança; não é uma
certificação de todos os arquivos, dependências ou plataformas.

O gateway recebe Telegram, WhatsApp e Pico; CLI também chama o agente. O
registro seleciona agente e sessão. Reflexos podem responder sem inferência;
perfis de foco selecionam contexto e ferramentas. O pipeline monta contexto,
consulta o provider/fallback, executa ferramentas e persiste a resposta.

Cada agente tem workspace, sessões, modelo e registro de ferramentas. Seahorse
é um backend opcional compartilhado de contexto em SQLite/FTS5; suas consultas
precisam preservar a sessão de origem. A inferência nativa usa llama.cpp via
cgo, com execução serializada, cache de prefixo e keep-alive. `runstate` controla
ocupação, suspensão e preempção do sonho por inferência do usuário.

Sono consolida histórico em `memory/MEMORY.md`; evolução organiza aprendizado.
Ambos exigem modelo externo. Cron, heartbeat, telemetria, monitoramento,
proteção de memória e integração systemd já possuem implementação.

## Incidente de configuração em produção

Inspeção do processo em execução antes de qualquer alteração:

- Binário: `/opt/kuromatsu/bin/kuromatsu`, build de 2026-09-14, versão `dev`.
- Diretório de trabalho e `KUROMATSU_HOME`: `/var/lib/kuromatsu`.
- Configuração principal: `/var/lib/kuromatsu/config.json`.
- Entrada principal: `bonsai-local`, provider `native`, modelo `Bonsai-1.7B-Q1_0`.
- GGUF efetivamente mapeado pelo processo: `/var/lib/kuromatsu/models/Bonsai-1.7B-Q1_0.gguf`.
- Cópia obsoleta: `/var/lib/kuromatsu/workspace/config.json`, com workspace
  Windows e entradas antigas `ollama/bonsai_8b:q1_0` e `openai/bonsai_8b_q1_0`.

A seleção de arquivo é `KUROMATSU_CONFIG`, se definido; caso contrário,
`GetHome()/config.json`. O diretório de trabalho não substitui essa seleção.
Logo, a cópia do workspace não prova uso do modelo 8B: o mapeamento do processo
confirma inferência local 1,7B. A mensagem relatada `Provider: openai` é uma
inconsistência de exibição/default legado, não evidência de inferência OpenAI.
CLI e gateway sobrescreviam `agents.defaults.model_name` com o nome físico
retornado pela fábrica de providers. Isso perdia o alias de `model_list` e fazia
a resolução posterior recorrer ao provider padrão (`openai`). A inicialização
e a recarga agora preservam o alias; o candidato mantém o provider explícito.

Correções: `/show model` obtém nome e provider do candidato resolvido;
`/show config` identifica arquivo ativo e workspace padrão sem imprimir
credenciais; startup registra a origem e avisa sobre uma cópia ignorada no
workspace; carregamento rejeita workspaces absolutos Windows em outros SOs.
O serviço passa a declarar `KUROMATSU_CONFIG` explicitamente. O deploy preserva
a cópia obsoleta em backup privado fora do workspace antes de removê-la.

## Registro dos achados e correções

| ID | Prioridade | Achado | Correção nesta rodada |
|---|---|---|---|
| SEC-01 | P1 | `short_grep` omitia `ConversationID`; `short_expand` aceitava IDs de outras conversas | Contexto confiável de sessão obrigatório, consultas delimitadas e expansão autorizada; pedido de acesso global recusado |
| SEC-02 | P1 | Sessões de shell globais sem proprietário; política remota apenas em `run` | Proprietário por agente/sessão/canal, filtragem da listagem e autorização em todas as ações |
| MEM-01 | P1 | Coleta avançava cursor antes da consolidação; contagem falhava após truncamento | Revisão do conteúdo, confirmação após persistência, cursor salvo em `state/sleep-cursors.json`, pendências preservadas |
| CFG-01 | P2 | Aliases nativos passavam como modelos externos | Aliases compartilhados com catálogo; validação de provider explícito e prefixo legado |
| MEM-02 | P2 | Orçamento contabilizado só depois da chamada; uso ausente era zero | Reserva conservadora de entrada, limite de saída enviado ao provider, lotes reduzidos para caber e estimativa quando usage faltar |
| MEM-03 | P2 | Fim da janela não cancelava consolidação em andamento | Deadline propagado até a chamada ao modelo; cancelamento impede persistir/confirmar resultado |
| SEC-03 | P2 | Argumentos e respostas de erro das ferramentas iam integralmente ao log | Registro central guarda metadados; conteúdo do erro fica na resposta ao chamador |
| CFG-02 | P2 | Validadores de horário discordavam | Configuração reutiliza o parser do agendamento |
| CFG-03 | P2 | Cópia de configuração Windows no workspace e provider exibido inconsistente | Proveniência explícita, teste do arquivo sombra, validação de caminhos e correção operacional |
| CONC-01 | P2 | `-race` confirmou publicação insegura do assembler Seahorse; compactador usava padrão equivalente | Leitura e inicialização sob mutex, incluindo leitura no encerramento |
| AGENT-01 | P2 | Spawner interno herdava só modelo do alvo | Transmite `TargetAgentID`, valida autorização e usa ferramentas do alvo |
| NATIVE-01 | P2 | Requisição já cancelada ainda podia carregar o GGUF | Verificação de contexto antes/depois do lock e após carregamento; teste de regressão |

Arquivos centrais: `pkg/seahorse/tool_scope.go`, `pkg/tools/shell.go`,
`pkg/agent/sleep_bridge.go`, `pkg/sleep/{triage,runtime,budget}.go`,
`pkg/config/{sleep,platform_paths}.go`, `pkg/agent/agent_command.go`.

## Garantias e limites

- Isolamento de ferramentas usa identidade fornecida pelo runtime, não argumentos
  gerados pelo modelo. APIs internas de armazenamento continuam disponíveis ao
  código confiável; não são uma interface de autorização para chamadas externas.
- Consolidação confirma apenas lotes persistidos. Falha entre persistência da
  memória e confirmação do cursor pode repetir trabalho; a semântica é ao menos
  uma vez, não uma transação única entre vários arquivos.
- A reserva de tokens é conservadora por bytes, com margem de framing. O teto
  enviado ao provider depende do cumprimento de `max_tokens` pelo provider;
  não é garantia de faturamento contra um serviço que ignore esse parâmetro.
- A validação de caminhos cobre workspaces operacionais. Não tenta converter
  automaticamente todos os caminhos de uma configuração entre Windows e Linux.
- Remoção dos argumentos no registro central não certifica anonimização de
  todos os logs de canais e provedores. Revisão de privacidade ampla continua
  sendo trabalho separado.
- Carregamento nativo do modelo ainda é síncrono: cancelar durante uma carga
  fria pode aguardar sua conclusão. O teste de cancelamento durante prefill
  agora aquece o modelo antes de medir; antes incluía equivocadamente o tempo
  de carregamento. O callback C usa flag global e merece isolamento por engine
  antes de suportar inferência concorrente entre múltiplos modelos nativos.

## Estado real versus documentação anterior

O backlog anterior está desatualizado em relação a: integração do sono,
validação de configuração, publicação de binários nativos, script de deploy,
verificação de atividade antes do backup, retenção de arquivos e propagação
do contexto de shutdown. Esses itens têm código. As skills legadas `hardware`
e `picoclaw-agent` já não constam no workspace.

Permanecem: skill `kuromatsu-docs`, sono configurável por agente, documentação
consolidada S36–S38, ensaio de restauração de backup e medições de desempenho
em hardware real. Backup por `tar` de SQLite em atividade merece validação de
consistência; esta rodada não afirma que restaurar foi testado.

## Duplicidade e manutenção

Foram unificadas as regras de horário e aliases nativos. O spawner interno agora
transporta a identidade do alvo. Fachadas de compatibilidade não foram removidas
em massa. A coexistência de `DispatchRequest` e campos legados, assim como o
tamanho de `shell.go`, `web.go` e `gateway.go`, permanece dívida de manutenção;
removê-los exige migração própria, além das correções funcionais desta rodada.

## Validação e implantação

Testes adicionados cobrem busca/expansão entre conversas, sessões de shell entre
agentes, confirmação após falha, cancelamento, persistência do cursor e
truncamento, aliases nativos, horários inválidos, caminhos Windows, seleção de
configuração e exibição do modelo. O teste existente `TestAssemblerLazyInitRace`
reproduziu CONC-01 antes da correção.

Resultados locais finais (logs em `build/audit/`, não versionados):

- `go mod verify`: todos os módulos verificados.
- `CGO_ENABLED=0 go test -p 2 -tags goolm,stdjson ./...`: 74 pacotes com
  testes aprovados, além dos pacotes sem testes.
- `go vet -p 2 -tags goolm,stdjson ./...`: aprovado.
- golangci-lint 2.10.1, `--build-tags goolm,stdjson --new-from-rev HEAD`:
  zero ocorrências novas. Esta execução é incremental, não uma certificação
  de ausência de dívida de lint em todo o histórico.
- `go test -race` em `pkg/seahorse`, `pkg/tools`, `pkg/sleep` e `pkg/config`:
  aprovado; a falha de inicialização do Seahorse foi reproduzida e corrigida.
- Suíte `pkg/providers/localllm` com `nativellm`, cgo e GGUF real habilitados:
  65 testes de nível principal aprovados, incluindo inferência, overflow,
  recarga, cancelamento durante prefill, cache de prefixo e core parking.
- CLI do binário final testada com default `openai` e entrada explícita
  `native`: `/show model` e `/show config` aprovados.
- `git diff --check`: aprovado. Scripts Python de implantação/verificação
  analisados sintaticamente.
- Suíte Docker não executada: integração Docker Desktop indisponível no WSL.

Implantação em `demetrius` em 2026-09-16:

- Versão do binário: `audit-20260916`, identificação `1aa84c95-audit`.
- SHA-256: `a54ef11ac1093961bea5c287e813c7d8d83e665252ecf9fb5eb548ff6c383ba5`.
- Backup privado: `/var/backups/kuromatsu/audit-20260916-7q6rmktf`.
  Contém binário anterior, configuração principal e cópia legada do workspace.
- `agents.defaults.provider` alinhado para `native`; entrada principal
  `bonsai-local`/`Bonsai-1.7B-Q1_0` preservada.
- Drop-in `/etc/systemd/system/kuromatsu.service.d/90-config-source.conf`
  define `KUROMATSU_HOME` e `KUROMATSU_CONFIG` explicitamente.
- Cópia obsoleta retirada do workspace após backup; serviço reiniciado e
  prontidão HTTP 200 confirmada. Não foi necessário acionar a reversão.
- Verificação posterior confirmou checksum instalado, serviço ativo,
  `KUROMATSU_CONFIG` no ambiente real do processo, ausência da cópia obsoleta
  e GGUF `Bonsai-1.7B-Q1_0.gguf` mapeado pelo gateway reiniciado.
- Comandos executados via CLI do binário instalado, como usuário `kuromatsu`,
  com a mesma configuração de produção (sem enviar mensagens aos canais):

  ```text
  Current Model: Bonsai-1.7B-Q1_0 (Provider: native)
  Active config: /var/lib/kuromatsu/config.json
  Default workspace: /var/lib/kuromatsu/workspace
  ```

Scripts reproduzíveis: `scripts/audit-runtime-config.py`,
`scripts/deploy-audit-demetrius.py` e `scripts/verify-audit-demetrius.py`.
O script de deploy exige checksum, valida o alvo, preserva os arquivos e tenta
restaurá-los automaticamente se a inicialização ou a prontidão falhar.

## Incidente complementar — heartbeat nativo em 2026-09-17

**NATIVE-02 (P1): cache de núcleos bloqueava prompts válidos.** A correção de
provider de 16/09 não cobria este defeito independente. O heartbeat já usava
`NoHistory`; não houve evidência de histórico de conversas acumulado entre
execuções. A configuração real tinha `n_ctx=4096` e `core_cache_parking=true`.

O buffer KV unificado é compartilhado pelo turno ativo e pelos núcleos
estacionados. O engine somava núcleos antigos ao orçamento, mas só os removia
ao substituir um slot, depois de decodificar o novo prompt. Quando faltava
espaço antes da decodificação, retornava overflow e preservava esses núcleos:
as tentativas seguintes encontravam o mesmo bloqueio. A mudança da data no
system prompt e a alternância cron/heartbeat geram núcleos distintos.

Evidências do servidor: falhas com `output_tokens_so_far=0`,
`n_ctx_used=2266` e limite 4096; na troca de tarefa à meia-noite, contagem 4160.
O teste `TestCgoEngine_CoreParkingEvictsUnderPressure_Integration`, usando o
GGUF real e janela 512, reproduziu a falha no segundo núcleo: cada prompt tinha
313 tokens e cabia sozinho, mas o núcleo anterior ocupava 301 posições.

Correção: antes de recusar um batch por capacidade, remover núcleos inativos
na ordem do menos recentemente usado. O núcleo ativo permanece protegido;
overflow real continua sendo erro. Não aumenta `n_ctx`, não trunca instruções,
não apaga memória persistente e mantém o cache habilitado.

**Desempenho separado:** execuções anteriores completaram chamadas a cerca
de 0,41–0,48 tokens/s; os logs mostram múltiplas iterações em alguns turnos.
A remoção do bloqueio do cache não implica resolver essa lentidão de inferência.
O limite configurado de 50 iterações também merece revisão por tarefa.

Validação desta correção: suíte nativa completa com GGUF real aprovada,
incluindo a regressão que falhava antes da alteração e os testes anteriores
de cache, cancelamento, recarga e overflow real. `go vet` nativo aprovado;
golangci-lint incremental com `goolm,stdjson,nativellm`: zero ocorrências.
Logs: `build/audit/heartbeat-regression-before.log`,
`heartbeat-native-tests.log`, `heartbeat-vet.log` e `heartbeat-lint.log`.

Implantação complementar em 17/09: versão `heartbeat-20260917`, SHA-256
`beaf166d62cd7b47a4800fed448db0c453a12e6ac66e682b6aa4bef98bbeb87b`.
Backup: `/var/backups/kuromatsu/audit-20260917-okay2cai`. Os 66 testes nativos
passaram. Checksum remoto, `/ready`, `/show model` e `/show config` verificados.
Janela 4096 e `core_cache_parking=true` preservados. O heartbeat iniciou às
13:43:14 UTC; sua conclusão é uma verificação separada da prontidão HTTP.

**Resultado observado em produção:** o heartbeat concluiu às 14:31:55 UTC de
17/09 com `Heartbeat OK - silent`, após 48min41s. A observação acompanhou a
execução natural iniciada pelo serviço; não disparou mensagens de diagnóstico
para canais. A regressão com múltiplos núcleos reproduziu a causa e validou a
correção localmente; a execução real confirmou retomada funcional. Ainda não
houve observação de uma nova virada de data após este deploy.

**Pendência de desempenho:** 48min41s é excessivo para uma checagem horária.
Separar custo de prefill, geração e iterações de ferramentas; avaliar um fluxo
determinístico para verificações simples e limites específicos por tarefa.
Não reduzir instruções, mudar modelo ou aumentar memória automaticamente como
parte da correção do cache. O defeito de capacidade foi corrigido; a latência
não foi resolvida nesta intervenção.

Verificação posterior até 17:56:01 UTC: **cinco heartbeats concluídos**, 25
chamadas nativas finalizadas, serviço ativo e **zero novos
`context_length_exceeded`** nesta execução do serviço. Além do primeiro ciclo,
houve conclusões às 15:55:52, 16:17:05, 17:42:34 e 17:56:01. As durações dos
cinco ciclos variaram de 12min48s a 72min39s; um ciclo ultrapassou o intervalo
horário. A última chamada registrou 2.644 tokens de entrada, 2.599 em cache,
79 de saída, prefill de 67,2s e geração de 162,3s (aproximadamente 0,49 token/s).

## Mensagem espontânea no Telegram — noite de 17/09

A resposta em inglês alegando que `exec` não está instalado veio do cron
`a7941f53305cdf3d`, nome **Heartbeat check executed**, habilitado com expressão
`0 0 * * *` no servidor. A execução iniciou em 18/09 às 00:00 UTC (21h de
17/09 em São Paulo); a resposta foi registrada às 00:06:26 UTC (21:06:26).

Os eventos registram `agent.tool.exec_start` e `agent.tool.exec_end` para
`tool=exec` às 00:03:50 UTC, com `error=true`, seguidos da resposta citada,
na mesma sessão cron. Portanto a ferramenta existia e foi chamada: a alegação
de instalação ausente é uma interpretação incorreta do modelo. A frase não
consta no log do serviço periódico de heartbeat. Trata-se de uma tarefa cron
com nome semelhante, não da comprovação de nova falha do cache nativo.

O motivo exato da falha de `exec` não foi recuperado: os eventos persistem
metadados de erro, sem argumentos/resultado integral, e essa sessão tem apenas
metadados no diretório de sessões. Não atribuir o erro a permissão, comando ou
ID de sessão sem evidência adicional. Revisar a intenção dessa tarefa e a
entrega de erros técnicos ao Telegram antes de alterar ou desativar a agenda.
