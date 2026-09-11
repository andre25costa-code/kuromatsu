---
id: S11
title: Requisitos funcionais e critérios de aceite
status: confirmed
version: 2
owner: André
last_updated: 2026-09-11
depends_on: [S06, S08, S10, S13, S18]
---

# S11 — Requisitos funcionais e critérios de aceite

**Gate**: nenhum código de feature é escrito sem o FR correspondente existir aqui.
Cada FR indica o entregável (E#) que o implementa. Regras citadas: ver S08. NFRs: ver S29.

---

### FR-001 — Inferência nativa in-process
**Entregável**: E4+E5 · **Módulos**: `pkg/providers/localllm` · **ADRs**: 001, 003

O binário compilado com a tag `nativellm` gera respostas a partir do GGUF local
(Bonsai-1.7B-Q1_0), in-process via cgo, sem servidor, sem rede, sem subprocesso.

- **AC-001-1** — Given binário `build-native` e GGUF presente, When `agent -m "olá"` com modelo `bonsai-local`, Then a resposta é gerada sem nenhuma conexão de rede.
- **AC-001-2** — Given binário padrão (sem a tag), When o provider nativo é invocado, Then retorna `ErrNotBuilt` com instrução de rebuild (`make build-native`).
- **AC-001-3** — Given prompt cujo total de tokens + margem de 64 excede `n_ctx`, When `Chat`, Then o erro é classificado como *context overflow* (não-retriable) e o pipeline aciona a sumarização existente.
- **AC-001-4** — Given duas chamadas `Chat` concorrentes, When executam, Then são serializadas por um único contexto llama (single-flight); nenhuma segunda instância do modelo é carregada.
- **AC-001-5** — Given `keep_alive_secs` decorridos sem uso, When expira o timer, Then o modelo é descarregado da RAM e recarregado sob demanda na chamada seguinte.

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

### FR-003 — Fallback nativo automático
**Entregável**: E6 · **Módulos**: `pkg/config/native_fallback.go` · **ADRs**: 002 · **Regras**: BR-002

Ao carregar o config, o sistema garante que o agente sempre tem um modelo utilizável:
sem chaves configuradas o modelo nativo vira o padrão; com chaves ele entra no fim da
fallback chain.

- **AC-003-1** — Given config sem nenhuma chave de API e GGUF presente (binário nativo), When `LoadConfig`, Then o modelo padrão efetivo é `bonsai-local`.
- **AC-003-2** — Given config com chave válida de um provedor, When `LoadConfig`, Then a chain resolve API primeiro e `bonsai-local` aparece como último fallback.
- **AC-003-3** — Given GGUF ausente **ou** binário sem `nativellm`, When `LoadConfig`, Then nada é adicionado à chain e nenhum candidato morto é tentado (BR-002).

### FR-004 — Diagnóstico do provider nativo
**Entregável**: E6 · **Módulos**: `cmd/*/internal/status`, `internal/model`

- **AC-004-1** — Given qualquer binário, When `kuromatsu status`, Then existe a linha "Native (Bonsai)" com um de: ✓ caminho do GGUF / "built, model missing" + dica `make model-download` / "not built" + dica `make build-native`.
- **AC-004-2** — Given binário sem a tag, When `kuromatsu model bonsai-local`, Then o comando recusa com a mensagem de rebuild em vez de configurar um modelo inoperante.

### FR-005 — Build nativo reprodutível
**Entregável**: E5+E7 · **Módulos**: `Makefile`, `docker/Dockerfile.native` · **ADRs**: 001, 009, 011 · **Regras**: BR-004

- **AC-005-1** — Given o submodule pinado, When `make llama-lib && make build-native` (Linux/WSL), Then o binário linka as `.a` estáticas e roda sem `.so` externos.
- **AC-005-2** — Given o Dockerfile nativo, When rodado via `.github/workflows/build-native-image.yml` (runner `ubuntu-24.04-arm`) ou via `docker buildx build --platform linux/arm64 -f docker/Dockerfile.native` num host amd64, Then a imagem resulta com o binário cgo para arm64 e **sem** o GGUF embutido.
- **AC-005-3** — Given `make build` (padrão), When executa em qualquer GOOS/GOARCH da matriz atual, Then compila com CGO_ENABLED=0 e o stub responde `ErrNotBuilt` (NFR-003).
- **AC-005-4** — Given o estágio builder do `Dockerfile.native`, When compila `ggml`/`llama.cpp` e o binário Go para arm64, Then roda nativamente (`gcc`/`g++` no runner arm64 do GitHub, ou cross-toolchain `aarch64-linux-gnu-gcc`/`g++` num host amd64, ambos via `--platform=$BUILDPLATFORM`) — nenhuma compilação C++ roda sob emulação QEMU (ADR-011).
- **AC-005-5** — Given a imagem `kuromatsu:native-arm64` já construída (via CI ou local), When entregue ao servidor, Then o caminho é baixar/gerar o `.tar.gz` + `scp` + `docker load` — a Oracle nunca executa um build.

### FR-006 — Workspace template + embed seguro
**Entregável**: E1 · **Módulos**: `workspace/`, `onboard_workspace_embed.go` · **Regras**: BR-003

- **AC-006-1** — Given o repo após E1, When `git ls-files workspace/`, Then só existem arquivos do template genérico PT-BR (sem hub_core, sem data/, sem sessões).
- **AC-006-2** — Given binário compilado, When inspecionado (`strings`), Then não contém conteúdo do hub pessoal.
- **AC-006-3** — Given `kuromatsu onboard` num home vazio, When executa, Then o workspace semeado é o template.

### FR-007 — Poda de escopo
**Entregável**: E2 · **Módulos**: `web/`, `pkg/channels/*`, `pkg/tools/hardware` · **ADRs**: 007

- **AC-007-1** — Given cada commit de remoção, When `make vet && make test`, Then verdes (sem referências órfãs).
- **AC-007-2** — Given a poda completa, When registrado no S40, Then constam tamanho do binário antes/depois e contagem de pacotes.

### FR-008 — Rebrand com compatibilidade
**Entregável**: E3 · **Módulos**: `go.mod`, `pkg/env.go`, `pkg/config/envkeys.go` · **ADRs**: 005

- **AC-008-1** — Given o rename completo, When `go build ./...`, Then o módulo é `github.com/andre25costa-code/kuromatsu` e o binário chama `kuromatsu`.
- **AC-008-2** — Given deploy antigo com `PICOCLAW_HOME` e/ou `~/.picoclaw`, When o novo binário sobe, Then funciona lendo o local antigo e loga aviso de deprecação.
- **AC-008-3** — Given envs `PICOCLAW_*` definidas e `KUROMATSU_*` ausentes, When `LoadConfig`, Then os valores antigos são honrados (shim).

### FR-009 — Skill kuromatsu-docs
**Entregável**: E8 · **Módulos**: `workspace/skills/kuromatsu-docs/` · **ADRs**: 010

- **AC-009-1** — Given a skill instalada, When o agente executa `find_skills`, Then `kuromatsu-docs` aparece e o SKILL.md navega por árvore de decisão para o tópico certo.
- **AC-009-2** — Given qualquer tópico (config, modelos, canais, tools, cron/heartbeat, sono, RAM), When procurado, Then existe **exatamente um** arquivo canônico que o documenta (唯一归属).
- **AC-009-3** — Given o repo, When `git ls-files '*README*'`, Then só o `README.md` raiz permanece.

### FR-010 — Modo dormir (consolidação noturna)
**Entregável**: E9 · **Módulos**: `pkg/sleep`, `pkg/agent/sleep_bridge.go` · **ADRs**: 006, 008 · **Regras**: BR-001, BR-005, BR-006

Config `sleep: {enabled, window, unconscious_model, weekly_deep, max_tokens_budget, dry_run}`.
Pipeline: coleta (sessões JSONL + seahorse + `.learnings/`) → triagem via LLM (map-reduce)
→ aplica (MEMORY.md atômico, compactação seahorse, relatório `sleep-report-<data>.md`).

- **AC-010-1** — Given `enabled: false` (default), When o gateway sobe, Then nenhuma goroutine/timer de sono é criada (BR-005).
- **AC-010-2** — Given `dry_run: true`, When a janela dispara, Then apenas o relatório é escrito; MEMORY.md e seahorse intactos.
- **AC-010-3** — Given orçamento de tokens atingido no meio da triagem, When a próxima chamada LLM seria feita, Then a rodada para e o relatório parcial registra o corte (BR-006).
- **AC-010-4** — Given um turn de agente ativo no horário da janela, When o timer dispara, Then a rodada é pulada e re-agendada (BR-001).
- **AC-010-5** — Given `unconscious_model` vazio, When a triagem roda, Then usa a chain padrão do agente (API se houver chave; senão o Bonsai local).
- **AC-010-6** — Given domingo e `weekly_deep: true`, When a janela dispara, Then a rodada executa a reorganização completa em vez da incremental.

### FR-011 — Tool sysmon
**Entregável**: E10 · **Módulos**: `pkg/tools/sysmon.go` · **Regras**: BR-007

- **AC-011-1** — Given o container com `mem_limit`, When `sysmon mem`, Then reporta uso e limite do cgroup (não o total do host).
- **AC-011-2** — Given config padrão, When `sysmon proc kill <pid>`, Then a ação é negada com "ação desabilitada" (BR-007).
- **AC-011-3** — Given `sysmon top`, When executa, Then lista os N processos por RSS com pid/nome/RSS.

### FR-012 — Download verificado do modelo
**Entregável**: E1 · **Módulos**: `scripts/download-model.sh`, `Makefile` · **ADRs**: 009 · **Depende**: S25 (fonte oficial — TBD)

- **AC-012-1** — Given `make model-download` sem o arquivo, When executa, Then baixa o GGUF para `models/` e valida o SHA256.
- **AC-012-2** — Given o arquivo já presente e íntegro, When re-executa, Then não baixa de novo (idempotente).
- **AC-012-3** — Given checksum divergente, When valida, Then o arquivo é rejeitado com erro claro.
