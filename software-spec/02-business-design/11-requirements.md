---
id: S11
title: Requisitos funcionais e critérios de aceite
status: confirmed
version: 8
owner: André
last_updated: 2026-09-12
depends_on: [S06, S08, S09, S10, S13, S16, S17, S18, S21, S27, S29, S30, S32, S33, S34]
---

# S11 — Requisitos funcionais e critérios de aceite

**Gate**: nenhum código de feature é escrito sem o FR correspondente existir aqui.
Cada FR indica o entregável que o implementa — `E#` para o plano herdado ou
**Trilho A/B/C/D/0** para o plano `demetrius` (ver nota de status abaixo). Regras
citadas: ver S08. NFRs: ver S29.

**Nota de auditoria (2026-09-11)**: FR-001..008, FR-011 e FR-012 têm entregável
fechado (código + teste; ver `entregues/E0..E7-*.md` e commit `9d891ab9` para o
FR-011/E10) e estão recolhidas em `<details>` abaixo — abrir para ver o texto
completo, nada foi removido. FR-009 e FR-010 permanecem **abertas** e visíveis
sem scroll (ver `BACKLOG.md`).

> **Nota de status (v7, 2026-09-12)**: **FR-013..020 confirmadas por André** ("pode
> mandar brasa", 2026-09-12) — gate da metodologia liberado; ADR-013..018 promovidas
> de `proposed` para `accepted` no mesmo momento. FR-001..012 permanecem no grau de
> confirmação que já tinham (auditoria acima). Correção da rodada anterior (v6,
> achado do `spec-verifier`): AC-020-2 (`MemoryMax` real = 900M, não 850M) e AC-020-7
> (arquivo real = `99-z-kuromatsu.conf`, não `90-kuromatsu.conf`) corrigidas para bater
> com a 1ª implantação real na `demetrius`; AC-020-8/9 acrescentadas com achados dessa
> mesma implantação. FR-010 tem 3 ACs novas (AC-010-7..9) refletindo a ADR-018.
> Onda 2 (código, Trilho B→A→C) autorizada a começar.

---

<details>
<summary><strong>FR-001 — Inferência nativa in-process</strong> — entregue (E4+E5)</summary>

### FR-001 — Inferência nativa in-process
**Entregável**: E4+E5 · **Módulos**: `pkg/providers/localllm` · **ADRs**: 001, 003

O binário compilado com a tag `nativellm` gera respostas a partir do GGUF local
(Bonsai-1.7B-Q1_0), in-process via cgo, sem servidor, sem rede, sem subprocesso.

- **AC-001-1** — Given binário `build-native` e GGUF presente, When `agent -m "olá"` com modelo `bonsai-local`, Then a resposta é gerada sem nenhuma conexão de rede.
- **AC-001-2** — Given binário padrão (sem a tag), When o provider nativo é invocado, Then retorna `ErrNotBuilt` com instrução de rebuild (`make build-native`).
- **AC-001-3** — Given prompt cujo total de tokens + margem de 64 excede `n_ctx`, When `Chat`, Then o erro é classificado como *context overflow* (não-retriable) e o pipeline aciona a sumarização existente.
- **AC-001-4** — Given duas chamadas `Chat` concorrentes, When executam, Then são serializadas por um único contexto llama (single-flight); nenhuma segunda instância do modelo é carregada.
- **AC-001-5** — Given `keep_alive_secs` decorridos sem uso, When expira o timer, Then o modelo é descarregado da RAM e recarregado sob demanda na chamada seguinte.

</details>

<details>
<summary><strong>FR-002 — Render e parse ChatML/Qwen3 em Go puro</strong> — entregue (E4)</summary>

### FR-002 — Render e parse ChatML/Qwen3 em Go puro
**Entregável**: E4 · **Módulos**: `pkg/providers/localllm/chatml.go` · **ADRs**: 004

`RenderPrompt` produz o prompt no formato exato do `tokenizer.chat_template` do GGUF
(system + bloco `# Tools`/`<tools>` + turnos; resultados de tool como `<tool_response>`
em turno user; prefill `<think>\n\n</think>` para suprimir thinking). `ParseOutput`
extrai content, reasoning e tool calls do texto gerado.

- **AC-002-1** — Given mensagens + `[]ToolDefinition`, When `RenderPrompt`, Then a saída bate com o golden file (system, `<tools>` com um JSON por função, instrução `<tool_call>`).
- **AC-002-2** — Given saída do modelo com `<tool_call>{"name":"x","arguments":{...}}</tool_call>`, When `ParseOutput`, Then retorna `ToolCall{Name, Arguments}` válido e `FinishReason` equivalente a tool_calls.
- **AC-002-3** — Given `<tool_call>` com JSON malformado, When `ParseOutput`, Then o call vem com `Arguments{"raw": <texto>}` (paridade com openai_compat) e nada explode.
- **AC-002-4** — Given saída com `<think>…</think>`, When `ParseOutput`, Then o miolo vai para reasoning e é removido do content.
- **AC-002-5** — Given histórico multi-turn com tool calls e tool responses, When `RenderPrompt`, Then turnos consecutivos de tool response são fundidos num único turno user (formato Qwen3).

</details>

<details>
<summary><strong>FR-003 — Fallback nativo automático</strong> — entregue (E6)</summary>

### FR-003 — Fallback nativo automático
**Entregável**: E6 · **Módulos**: `pkg/config/native_fallback.go` · **ADRs**: 002 · **Regras**: BR-002

Ao carregar o config, o sistema garante que o agente sempre tem um modelo utilizável:
sem chaves configuradas o modelo nativo vira o padrão; com chaves ele entra no fim da
fallback chain.

- **AC-003-1** — Given config sem nenhuma chave de API e GGUF presente (binário nativo), When `LoadConfig`, Then o modelo padrão efetivo é `bonsai-local`.
- **AC-003-2** — Given config com chave válida de um provedor, When `LoadConfig`, Then a chain resolve API primeiro e `bonsai-local` aparece como último fallback.
- **AC-003-3** — Given GGUF ausente **ou** binário sem `nativellm`, When `LoadConfig`, Then nada é adicionado à chain e nenhum candidato morto é tentado (BR-002).

</details>

<details>
<summary><strong>FR-004 — Diagnóstico do provider nativo</strong> — entregue (E6)</summary>

### FR-004 — Diagnóstico do provider nativo
**Entregável**: E6 · **Módulos**: `cmd/*/internal/status`, `internal/model`

- **AC-004-1** — Given qualquer binário, When `kuromatsu status`, Then existe a linha "Native (Bonsai)" com um de: ✓ caminho do GGUF / "built, model missing" + dica `make model-download` / "not built" + dica `make build-native`.
- **AC-004-2** — Given binário sem a tag, When `kuromatsu model bonsai-local`, Then o comando recusa com a mensagem de rebuild em vez de configurar um modelo inoperante.

</details>

<details>
<summary><strong>FR-005 — Build nativo reprodutível</strong> — entregue (E5+E7)</summary>

### FR-005 — Build nativo reprodutível
**Entregável**: E5+E7 · **Módulos**: `Makefile`, `docker/Dockerfile.native` · **ADRs**: 001, 009, 012 · **Regras**: BR-004

- **AC-005-1** — Given o submodule pinado, When `make llama-lib && make build-native` (Linux/WSL), Then o binário linka as `.a` estáticas e roda sem `.so` externos.
- **AC-005-2** — Given o Dockerfile nativo, When rodado via `.github/workflows/build-native-image.yml` (runner `ubuntu-latest`, amd64) ou via `docker buildx build -f docker/Dockerfile.native` num host amd64, Then a imagem resulta com o binário cgo para amd64 e **sem** o GGUF embutido.
- **AC-005-3** — Given `make build` (padrão), When executa em qualquer GOOS/GOARCH da matriz atual, Then compila com CGO_ENABLED=0 e o stub responde `ErrNotBuilt` (NFR-003).
- **AC-005-4** — Given o estágio builder do `Dockerfile.native`, When compila `ggml`/`llama.cpp` e o binário Go, Then usa `GGML_NATIVE=OFF` com `AVX2+FMA+F16C` fixos e `AVX-512`/`AVX-VNNI` desligados — nunca auto-detecta a CPU do runner de build, que pode ter instruções que a Oracle (Zen1, EPYC 7551) não tem (ADR-012).
- **AC-005-5** — Given a imagem `kuromatsu:native-amd64` já construída (via CI ou local), When entregue ao servidor, Then o caminho é baixar/gerar o `.tar.gz` + `scp` + `docker load` — a Oracle nunca executa um build.
- **AC-005-6** — Given o suporte ARM64 legado (`llama-lib-arm64`/`build-native-arm64`, ADR-011), When invocado manualmente, Then continua funcional como caminho secundário (ex. Raspberry Pi) — não é mais o caminho usado pelo `Dockerfile.native`/workflow padrão.

</details>

<details>
<summary><strong>FR-006 — Workspace template + embed seguro</strong> — entregue (E1)</summary>

### FR-006 — Workspace template + embed seguro
**Entregável**: E1 · **Módulos**: `workspace/`, `onboard_workspace_embed.go` · **Regras**: BR-003

- **AC-006-1** — Given o repo após E1, When `git ls-files workspace/`, Then só existem arquivos do template genérico PT-BR (sem hub_core, sem data/, sem sessões).
- **AC-006-2** — Given binário compilado, When inspecionado (`strings`), Then não contém conteúdo do hub pessoal.
- **AC-006-3** — Given `kuromatsu onboard` num home vazio, When executa, Then o workspace semeado é o template.

</details>

<details>
<summary><strong>FR-007 — Poda de escopo</strong> — entregue (E2)</summary>

### FR-007 — Poda de escopo
**Entregável**: E2 · **Módulos**: `web/`, `pkg/channels/*`, `pkg/tools/hardware` · **ADRs**: 007

- **AC-007-1** — Given cada commit de remoção, When `make vet && make test`, Then verdes (sem referências órfãs).
- **AC-007-2** — Given a poda completa, When registrado no S40, Then constam tamanho do binário antes/depois e contagem de pacotes.

</details>

<details>
<summary><strong>FR-008 — Rebrand com compatibilidade</strong> — entregue (E3)</summary>

### FR-008 — Rebrand com compatibilidade
**Entregável**: E3 · **Módulos**: `go.mod`, `pkg/env.go`, `pkg/config/envkeys.go` · **ADRs**: 005

- **AC-008-1** — Given o rename completo, When `go build ./...`, Then o módulo é `github.com/andre25costa-code/kuromatsu` e o binário chama `kuromatsu`.
- **AC-008-2** — Given deploy antigo com `PICOCLAW_HOME` e/ou `~/.picoclaw`, When o novo binário sobe, Then funciona lendo o local antigo e loga aviso de deprecação.
- **AC-008-3** — Given envs `PICOCLAW_*` definidas e `KUROMATSU_*` ausentes, When `LoadConfig`, Then os valores antigos são honrados (shim).

</details>

### FR-009 — Skill kuromatsu-docs
**Entregável**: E8 · **Módulos**: `workspace/skills/kuromatsu-docs/` · **ADRs**: 010

- **AC-009-1** — Given a skill instalada, When o agente executa `find_skills`, Then `kuromatsu-docs` aparece e o SKILL.md navega por árvore de decisão para o tópico certo.
- **AC-009-2** — Given qualquer tópico (config, modelos, canais, tools, cron/heartbeat, sono, RAM), When procurado, Then existe **exatamente um** arquivo canônico que o documenta (唯一归属).
- **AC-009-3** — Given o repo, When `git ls-files '*README*'`, Then só o `README.md` raiz permanece.

### FR-010 — Modo dormir (consolidação noturna)
**Entregável**: E9 (+ Trilho C, C5) · **Módulos**: `pkg/sleep`, `pkg/agent/sleep_bridge.go` · **ADRs**: 006, 008, 018 · **Regras**: BR-001, BR-005, BR-006

Config `sleep: {enabled, window, unconscious_model, weekly_deep, max_tokens_budget, dry_run}`.
Pipeline: coleta (sessões JSONL + seahorse + `.learnings/`) → triagem via LLM (map-reduce)
→ aplica (MEMORY.md atômico, compactação seahorse, relatório `sleep-report-<data>.md`).

- **AC-010-1** — Given `enabled: false` (default), When o gateway sobe, Then nenhuma goroutine/timer de sono é criada (BR-005).
- **AC-010-2** — Given `dry_run: true`, When a janela dispara, Then apenas o relatório é escrito; MEMORY.md e seahorse intactos.
- **AC-010-3** — Given orçamento de tokens atingido no meio da triagem, When a próxima chamada LLM seria feita, Then a rodada para e o relatório parcial registra o corte (BR-006).
- **AC-010-4** — Given um turn de agente ativo no horário da janela, When o timer dispara, Then a rodada é pulada e re-agendada (BR-001).
- **AC-010-5** — Given `unconscious_model` vazio, When a triagem roda, Then usa a chain padrão do agente (API se houver chave; senão o Bonsai local).
- **AC-010-6** — Given domingo e `weekly_deep: true`, When a janela dispara, Then a rodada executa a reorganização completa em vez da incremental.

**ACs novas (confirmadas, 2026-09-12 — ADR-018, plano `demetrius`)**:

- **AC-010-7** — Given `sleep.enabled: true` e `unconscious_model` vazio, ausente, **ou** resolvendo para `provider: native`, When `LoadConfig` valida (`ValidateSleep`), Then o gateway sobe normalmente com o Bonsai nativo para conversas, mas o sono fica desligado com o log `"modo dormir requer um modelo externo em sleep.unconscious_model"` — não é erro fatal de boot.
- **AC-010-8** — Given `unconscious_model` configurado e válido (não-nativo), When a janela de sono dispara, Then a triagem chama exclusivamente esse candidato (`ExecuteCandidate`, `tools=nil`), **nunca** cai na fallback chain nem no `bonsai-local` — diferente de AC-010-5 (comportamento antigo da ADR-006, agora superseded nesse ponto).
- **AC-010-9** — Given a evolução (`pkg/evolution`) habilitada e sem modelo externo configurado em `evolution.model`, When o timer de evolução dispara, Then a rodada não executa — mesma trava do sono, aplicada ao coldpath de evolução.

**Status (2026-09-11)**: parcialmente entregue. `pkg/sleep` (núcleo: janela,
triagem com teto de tokens, aplicação em MEMORY.md, relatório) existe e está
testado isoladamente — commit `4ff87eb8`, "E9, part 1/2". **Falta a parte 2**:
`pkg/agent/sleep_bridge.go` (o agendador real ligado ao processo do agente, o
bloco de config `sleep` e os adapters reais de `SessionSource`/`ChatFunc` —
sem eles, AC-010-1 e AC-010-4 não têm onde rodar em produção) **e agora também**
a validação `ValidateSleep`/AC-010-7..9 (ADR-018, `accepted`). Ver `BACKLOG.md`
(E9 parte 2) — o escopo da parte 2 cresceu para incluir a trava de modelo externo.

<details>
<summary><strong>FR-011 — Tool sysmon</strong> — entregue (E10)</summary>

### FR-011 — Tool sysmon
**Entregável**: E10 · **Módulos**: `pkg/tools/sysmon.go` · **Regras**: BR-007

- **AC-011-1** — Given o container com `mem_limit`, When `sysmon mem`, Then reporta uso e limite do cgroup (não o total do host).
- **AC-011-2** — Given config padrão, When `sysmon proc kill <pid>`, Then a ação é negada com "ação desabilitada" (BR-007).
- **AC-011-3** — Given `sysmon top`, When executa, Then lista os N processos por RSS com pid/nome/RSS.

</details>

<details>
<summary><strong>FR-012 — Download verificado do modelo</strong> — entregue (E1)</summary>

### FR-012 — Download verificado do modelo
**Entregável**: E1 · **Módulos**: `scripts/download-model.sh`, `Makefile` · **ADRs**: 009 · **Depende**: S25 (fonte oficial — confirmada em 2026-09-11; ver ADR-009 e commit `b2912789`)

- **AC-012-1** — Given `make model-download` sem o arquivo, When executa, Then baixa o GGUF para `models/` e valida o SHA256.
- **AC-012-2** — Given o arquivo já presente e íntegro, When re-executa, Then não baixa de novo (idempotente).
- **AC-012-3** — Given checksum divergente, When valida, Then o arquivo é rejeitado com erro claro.

</details>

---

## FR-013..020 — Plano `demetrius` (confirmadas por André em 2026-09-12)

Fonte única: `C:\Users\pc\.claude\plans\quero-refatorar-esse-projeto-twinkly-papert.md`
(plano aprovado no nível de arquitetura em 2026-09-11; FR/AC confirmadas FR-a-FR em
2026-09-12 — "pode mandar brasa"). Ver ADR-013..018 (`accepted`) e os capítulos novos
S09/S16/S17/S21/S27/S30/S33/S34 para o desenho completo — as ACs abaixo são o
contrato de aceite, não a explicação do mecanismo. Onda 2 (código, Trilho B→A→C)
liberada por este gate.

### FR-013 — Reflexos
**Entregável**: Trilho A (A5) · **Módulos**: `pkg/agent/reflex.go` · **ADRs**: 014

Caminho determinístico de **0 tokens** antes do roteador de foco (FR-014): regex →
`reply` (template), `command` (slash command existente) ou `exec` (mesmo gating de
segurança de `pkg/tools`). Chamado em `processMessage` após `handleCommand`, só para
mensagens de origem `user`.

- **AC-013-1** — Given uma mensagem que casa com um reflexo `action: reply`, When `processMessage` roda, Then a resposta é o template renderizado (`$1…`, `{{sender}}`, `{{time}}`) sem nenhuma chamada ao LLM.
- **AC-013-2** — Given um reflexo `action: command`, When a mensagem casa, Then o slash command correspondente executa via `handleCommand` e o resultado vira a resposta do turno.
- **AC-013-3** — Given um reflexo `action: exec`, When a mensagem casa, Then a tool `exec` é chamada via `Tools.ExecuteWithContext` herdando os mesmos deny patterns/restrict/timeouts já configurados — nenhum bypass de segurança.
- **AC-013-4** — Given `Persist` não definido, When o reflexo é `reply`/`command`, Then a sessão grava user+assistant (default `true`); When o reflexo é `exec`, Then não grava por padrão (default `false`).
- **AC-013-5** — Given nenhum reflexo configurado (`reflexes: []`, default), When qualquer mensagem chega, Then o comportamento é idêntico ao pipeline atual — sem overhead novo.
- **AC-013-6** — Given um reflexo disparado, When o turno completa, Then a telemetria (FR-019) registra o turno com `prompt_tokens=0`/`output_tokens=0`.

### FR-014 — Roteador de foco (janelas, escalonamento, heartbeat/cron)
**Entregável**: Trilho A (A1-A4) · **Módulos**: `pkg/config/focus.go`, `pkg/routing/focus.go`, `pkg/agent/{agent,focus,agent_message,agent_init,agent_command,turn_state,turn_coord}.go`, `pkg/commands/cmd_foco.go`, `pkg/tools/cron.go` · **ADRs**: 014

Roteador determinístico por mensagem (0 tokens de LLM) resolve uma janela de foco por
turno (tabela de janelas padrão em ADR-014/S16); escalonamento bounded (1×/turno)
quando o modelo pede uma tool fora da janela ativa **mas existente no catálogo
global**; heartbeat e cron recebem janelas próprias.

- **AC-014-1** — Given `focus.enabled: true` e uma mensagem sem tag/janela explícita, When o roteador aplica as regras na ordem configurada (keyword com fold de acento, depois regex), Then a primeira regra que casar decide a janela; nenhuma regra casando usa `Default`.
- **AC-014-2** — Given uma mensagem com a tag inline `[foco:files] leia o arquivo X`, When roteada, Then a janela `files` é usada e a tag é removida da mensagem persistida na sessão.
- **AC-014-3** — Given origem `heartbeat` ou `cron` (não `user`), When `applyFocus` roda, Then a janela é resolvida por `Origins`, não pelas regras de keyword/regex de usuário.
- **AC-014-4** — Given uma sessão que rodou na janela `files` no turno anterior e `sticky_turns: 3` para essa janela, When a próxima mensagem não casa explicitamente com nenhuma regra, Then a aderência mantém `files`.
- **AC-014-5** — Given o modelo chama uma tool que **não existe** no catálogo global do agente (nome inventado/alucinado), When `ExecuteTools` processa, Then a rejeição é imediata — sem escalar, sem tocar o KV — com dica de tool parecida por distância de edição entre as tools disponíveis na janela atual.
- **AC-014-6** — Given o modelo chama uma tool que **existe** no catálogo global mas está fora da janela ativa, e `focusEscalations < MaxPerTurn` (1), When `ExecuteTools` processa, Then a janela escala para `EscalateTo` (reconstruindo só a mensagem de sistema), `focusEscalations` incrementa, e a resposta rejeitada não é persistida.
- **AC-014-7** — Given `focusEscalations == MaxPerTurn` no turno, When outra tool fora da janela é pedida, Then `denyByTurnProfile` nega sem escalar de novo.
- **AC-014-8** — Given `/foco files pesquise algo`, When o comando roda, Then só essa mensagem é roteada para `files` sem armar aderência; Given `/foco files` sem mensagem, Then a aderência é armada para as próximas mensagens da sessão; Given `/foco auto` ou `/foco off`, Then a aderência é esquecida (`Forget`).
- **AC-014-9** — Given `focus.enabled: false` (default), When qualquer mensagem chega, Then o comportamento é idêntico ao `turn_profile` estático atual — bytes do system prompt e schemas inalterados, sem escalonamento.

### FR-015 — Prompt e schema de tools compactos
**Entregável**: Trilho A (A6-A8) · **Módulos**: `pkg/config/turn_profile.go`, `pkg/agent/{context,prompt,prompt_turn,turn_profile_policy}.go`, `pkg/providers/common/{tool_schema_transform,compact_schema}.go`, `pkg/config/native_fallback.go`, `pkg/agent/instance.go` · **ADRs**: 014

- **AC-015-1** — Given `system_prompt.mode: compact`, When o prompt é montado, Then a identidade compacta (`getIdentityCompact`) fica em ≤~150 tokens de overhead de framework.
- **AC-015-2** — Given `DynamicContext: "date"`, When o system prompt é montado, Then o bloco dinâmico contém só `## Current Date` (sem Runtime/Session/Sender, sem carimbo de minuto); Given `NeedsTime: true` na janela ativa, Then `[now: …]` é anexado ao final da mensagem de usuário montada, nunca persistido.
- **AC-015-3** — Given o mesmo dia e nenhuma mudança nos arquivos-fonte do workspace, When duas chamadas consecutivas montam o prompt compacto, Then o prefixo (identidade + tools + histórico antigo) é byte-idêntico entre elas.
- **AC-015-4** — Given `tool_schema_transform: compact`, When os schemas das tools são gerados, Then cada descrição vira a primeira frase (≤100 chars) e cada propriedade mantém só `type` (+ `enum`≤8, `items:{type}`) — nenhuma propriedade é removida (ex.: `exec` mantém `command` mesmo não sendo `required`).
- **AC-015-5** — Given as 19 tools do agente com schema compacto, When `EstimateToolDefsTokens` mede, Then o total fica bem abaixo do não-compactado — golden test com os schemas reais de `exec`, `sysmon`, `cron`: medido na 1ª implementação real (compilação/teste de verdade no `kuro`), a compactação já reduz o total em ≥40% e a média medida ficou em **168 tokens/tool** (não os "≤70" originais). O orçamento original desta AC presumia menos propriedades por tool do que `exec`/`cron` realmente têm (~9 cada); manter a decisão do ADR-014 de nunca remover propriedade não-obrigatória (ex.: `command` do `exec`) — que a própria ADR já justifica — torna 70/tool inalcançável sem violar essa decisão. Orçamento corrigido: **corte de pelo menos 40% frente ao não-compactado, e teto de 200/tool em média**, ambos medidos no golden test (`TestCompactSchema_GoldenExecSysmonCronUnder70TokensAverage`, nome do teste mantido por histórico).
- **AC-015-6** — Given `ExtraBody` do modelo nativo define `n_ctx`/`max_predict`, When `instance.go` monta as opções, Then `ContextWindow = n_ctx` (se não configurado) e `MaxTokens = min(MaxTokens, max_predict)` — o corte proativo de overflow passa a disparar antes do `ErrContextOverflow` real.
- **AC-015-7** — Given `tool_schema_transform` ausente para um modelo que não seja `bonsai-local`, When os schemas são gerados, Then o comportamento é verbatim (idêntico a hoje).

### FR-016 — Cache de prefixo do KV + núcleos de janela em RAM
**Entregável**: Trilho B (B0-B2) · **Módulos**: `pkg/providers/localllm/{types,engine_cgo,provider,prefix,chatml}.go`, `pkg/providers/protocoltypes/types.go`, `cmd/nativebench/main.go` · **ADRs**: 015

- **AC-016-1** — Given duas chamadas `Chat` consecutivas na mesma janela (mesmo system+tools, histórico crescente), When a 2ª chamada roda, Then `0 < CachedTokens < PromptTokens` (delta reprocessado, não o prompt completo).
- **AC-016-2** — Given uma 3ª chamada idêntica à 2ª, When roda, Then `CachedTokens == PromptTokens - 1`.
- **AC-016-3** — Given um erro ou abort durante o decode, When a chamada falha, Then `llama_memory_clear` é chamado e `kvTokens` é zerado — a próxima chamada reprocessa do zero, nunca com um KV parcialmente inconsistente.
- **AC-016-4** — Given duas chamadas com `max_tokens`/`temperature` diferentes mas o mesmo `loadKey`, When ambas rodam, Then `loadCount == 1` — nenhum reload do modelo entre elas.
- **AC-016-5** — Given um cancelamento de contexto 200 ms após o início de um prefill de ~1 500 tokens, When o `abort_callback` é acionado, Then a chamada retorna `context.Canceled` em menos de 3 s.
- **AC-016-6** — Given `options["focus_window"]` preenchido e o núcleo dessa janela já cacheado numa sequência `k` do KV unificado, When a janela é reativada, Then `llama_memory_seq_cp` restaura o núcleo em milissegundos e só o delta pós-núcleo é decodificado.
- **AC-016-7** — Given `kv_state_dir` configurado (≠ `"off"`) e `runstate` em `Idle`, When o processo reinicia, Then os núcleos das janelas mais usadas recarregam do disco (`llama_state_seq_load_file`) antes do serviço ficar `ready`, com `load_ms` medido e logado.
- **AC-016-8** — Given `kv_state_dir: "off"` (ou o gate `load_ms`/`iowait` falhando em produção), When o serviço sobe, Then só o mecanismo em RAM (núcleos via `seq_cp`) funciona — nenhuma leitura/escrita de estado do KV em disco.
- **AC-016-9** — Given `keep_alive_secs: -1`, When uma chamada termina, Then o modelo nunca é descarregado por timeout (só por `UnloadAll()` explícito).

### FR-017 — Motor de estados (`runstate`)
**Entregável**: Trilho C (C1-C2) · **Módulos**: `pkg/runstate/{mode,engine,publish_file,publish_sdnotify,publish_log}.go`, `pkg/heartbeat/service.go`, `pkg/health/server.go`, `pkg/gateway/gateway.go`, `pkg/agent/evolution_bridge.go`, `pkg/tools/sysmon.go` · **ADRs**: 016

- **AC-017-1** — Given `runstate.enabled: true`, When uma inferência inicia (`Inc(Inference)`), Then `run/state`, `/health` e `sd_notify STATUS=` refletem o bit `Inference` ativo.
- **AC-017-2** — Given `Inference` ativo, When o heartbeat dispara, Then ele é pulado (logado) e **não enfileirado**.
- **AC-017-3** — Given o estado atual é `Idle`, When a evolução ou o sono chamam `TryEnterDream`, Then a entrada é aceita (`Dream` ativa); Given qualquer outro bit já ativo, Then retorna `ErrBusy` e o chamador re-tenta (evolução: ~2 min; sono: até o deadline da janela).
- **AC-017-4** — Given `Dream` ativo, When uma inferência de usuário chega (`Inc(Inference)`), Then o `dreamCancel` aciona (sono/evolução são interrompidos) e a inferência do usuário prossegue sem esperar.
- **AC-017-5** — Given múltiplos `Inc(ToolExec)` concorrentes, When todas as chamadas liberam (`release()`), Then o bit `ToolExec` só desliga quando o refcount chega a 0.
- **AC-017-6** — Given `steal` (`/proc/stat`) acima de 50% sustentado por ≥60 s, When o vigilante mede, Then o bit `Throttled` liga (informativo, não bloqueia nada); Given `steal` abaixo de 25%, Then desliga.
- **AC-017-7** — Given `runstate.enabled: false` (default fora da demetrius), When o processo sobe, Then nenhuma goroutine/arquivo/`sd_notify` é criado — heartbeat nunca é pulado, `/health` mantém o formato atual sem o campo `state`.

### FR-018 — Guarda de memória (memguard)
**Entregável**: Trilho C (C3) · **Módulos**: `pkg/runstate/memguard{,_linux,_other}.go`, `pkg/sysinfo/`, `pkg/providers/localllm/{types,engine_cgo}.go` · **ADRs**: 017

- **AC-018-1** — Given o modelo carrega com sucesso, When `postLoad` roda, Then `debug.SetMemoryLimit` é ajustado para `Plan(est)` (cgroup/MemTotal − modelo − KV − compute − margem, clamp ≥128 MiB) e `debug.SetGCPercent(50)` é aplicado.
- **AC-018-2** — Given `MemAvailable < KV+compute+margem` no momento do load, When `PreLoad` roda, Then o carregamento é negado com erro de texto `overloaded`, classificado como `FailoverOverloaded`.
- **AC-018-3** — Given uma leitura de PSI com `some > 20`, When o vigilante roda, Then no máximo 1 GC/`FreeOSMemory` é disparado por minuto.
- **AC-018-4** — Given `full > 10%` sustentado por ≥30 s, When o vigilante detecta, Then `Suspend()` + `UnloadAll()` descarregam o modelo antes de um OOM; Given `full < 5%` por ≥30 s depois, Then `Resume()` é chamado.
- **AC-018-5** — Given um teste de pressão sintética simulando `full` alto, When a rodada completa, Then `dmesg | grep -i oom` fica vazio.
- **AC-018-6** — Given uma plataforma sem Linux/PSI (ex.: Windows), When o processo sobe, Then o memguard desliga com aviso e o `GOMEMLIMIT` estático (se configurado) continua honrado.

### FR-019 — Telemetria por turno + `/stats`
**Entregável**: Trilho C (C4) · **Módulos**: `pkg/telemetry/{store,recorder}.go`, `pkg/agent/telemetry_bridge.go`, `pkg/commands/cmd_stats.go` · **ADRs**: 017

- **AC-019-1** — Given `telemetry.enabled: true` e um turno concluído, When `KindAgentTurnEnd` é emitido, Then uma linha é gravada em `turns.db` com `ts, origin, session_key, window, escalations, unknown_tool_calls, prompt_tokens, cached_tokens, output_tokens, prefill_ms, gen_ms, total_ms, tools_called, iterations, status, mode_bits, rss_kb, steal_pct`.
- **AC-019-2** — Given o canal do `Recorder` (256) saturado, When um novo turno tenta gravar, Then o turno é descartado com um contador incrementado — nunca bloqueia o turno em andamento.
- **AC-019-3** — Given um reflexo (FR-013) disparado, When o turno completa, Then a linha gravada tem `prompt_tokens=0`/`output_tokens=0`.
- **AC-019-4** — Given `/stats files 24`, When o comando roda, Then a resposta agrega via SQL (contagem, prompt médio, % em cache, tempo médio, p50/p90) para a janela `files` nas últimas 24h.
- **AC-019-5** — Given `telemetry.retention_days: 30`, When a rotina diária de `Prune` roda, Then linhas com `ts` mais antigo que 30 dias são removidas.
- **AC-019-6** — Given `telemetry.enabled: false` (default), When qualquer turno completa, Then nenhum arquivo `turns.db` é criado.

### FR-020 — Deploy nativo sob systemd na `demetrius` (trim, sysctl, backup)
**Entregável**: Trilho 0 + Trilho D · **Módulos**: `deploy/{systemd,sysctl,nftables}/`, `scripts/deploy-demetrius.sh`, `.github/workflows/build-native-image.yml` · **ADRs**: 013

- **AC-020-1** — Given os serviços `google-cloud-ops-agent*`, `google-osconfig-agent`, `exim4`, `rsyslog`, `haveged` desativados e `fail2ban` substituído por rate-limit `nftables` na porta 22, When medido depois do trim, Then `MemAvailable` idle é ≥650 MB.
- **AC-020-2** — Given a unit `deploy/systemd/kuromatsu.service` (`MemoryMax=900M` — subido de 850M na 1ª implantação real: com `n_ctx` acima de 2048 [necessário até o Trilho A/B reduzirem o prompt-base] o KV `q8_0` sozinho passa de 850M com folga pequena; `CPUWeight=1000`, `Nice=-5`, `OOMScoreAdjust=-500`, `Restart=on-failure`, `NoNewPrivileges=true`, `ProtectSystem=full`+`ReadWritePaths=/var/lib/kuromatsu`), When instalada e um reboot ocorre, Then `kuromatsu.service` sobe automaticamente e `systemctl status` reporta `active (running)`.
- **AC-020-3** — Given o CI (`build-native-image.yml`), When o workflow roda, Then publica, além da imagem `kuromatsu:native-amd64`, os binários crus `kuromatsu-native-linux-amd64` e `nativebench-linux-amd64`.
- **AC-020-4** — Given `scripts/deploy-demetrius.sh`, When executado com um binário novo, Then `scp` + `systemctl restart kuromatsu` completam sem exigir Docker na `demetrius`.
- **AC-020-5** — Given `docker.socket docker containerd` desativados (não removidos), When `systemctl list-units` é consultado, Then Docker aparece instalado porém inativo — rollback é `systemctl enable --now docker`.
- **AC-020-6** — Given o timer noturno de backup (`kuromatsu-backup.timer`), When `runstate` está `Idle` no horário disparado, Then um `tar.zst` de `/var/lib/kuromatsu` é gerado e enviado via `rsync` para o `kuro` (Oracle); Given `runstate` não está `Idle`, Then o timer aguarda a próxima janela.
- **AC-020-7** — Given `/etc/sysctl.d/99-z-kuromatsu.conf` aplicado (`vm.swappiness=100`, `vm.page-cluster=0`, `vm.vfs_cache_pressure=50`, THP `madvise`) — nome escolhido na 1ª implantação real para ordenar depois de um `99-servidor.conf` legado já existente na VM (tuning antigo, de antes dela ser exclusiva do Kuromatsu) sem apagá-lo, quando `90-kuromatsu.conf` teria sido sobrescrito por ele —, When o sistema reinicia, Then os valores persistem.
- **AC-020-8** — Given o `.security.yml`/`config.json` reais implantados, When o gateway inicia, Then só canais que existem neste fork (`pico`/`telegram`/`whatsapp`) aparecem em `channel_list`; entradas de canais removidos no E2 (ex.: `feishu`, `maixcam`) fazem o loader rejeitar o boot com "unknown type" — confirmado na 1ª implantação real (`kuromatsu[NNNNN]: ERR ... channel "feishu" has unknown type`).
- **AC-020-9** — Given o workspace pessoal real (não um template genérico) carregado, When o `turn_profile` está desligado, Then o prompt de heartbeat pode superar bastante os ~2790 tokens estimados no R6 (medido na 1ª implantação real: 6029 tokens, com 17 tools + catálogo de skills reais) — o número de referência do R6/S06 é um piso do template, não um teto do workspace real.
