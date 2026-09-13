---
id: E4
title: Render e parse ChatML/Qwen3 em Go puro
status: done
frs: [FR-002]
adrs: [ADR-004]
commits: [5d2fe0fe]
last_updated: 2026-09-11
---

# E4 — ChatML/Qwen3 em Go puro

## O que foi entregue

Novo pacote `pkg/providers/localllm` com as partes do provider nativo que não
precisam de cgo (ADR-004):

- `chatml.go`: `RenderPrompt`/`ParseOutput` reproduzem o template ChatML/Qwen3
  gravado no `tokenizer.chat_template` do GGUF Bonsai (bloco `# Tools`,
  turnos `<tool_call>`/`<tool_response>`, prefill `<think></think>` vazio para
  desligar o "pensamento") — necessário porque `llama_chat_apply_template` não
  tem parâmetro de tools. JSON malformado em `<tool_call>` cai para
  `Arguments{"raw": ...}` (paridade com `openai_compat`); máximo 4 calls.
- `provider.go`: adapta o engine para `providers.LLMProvider` estruturalmente
  (via aliases de `protocoltypes`), com registry por caminho de GGUF — uma
  instância por modelo (ADR-003).
- `resolve.go`: `ResolveModelPath` busca caminho explícito → `$KUROMATSU_MODELS_DIR`
  → `<home>/models/` → `./models/`.
- `errors.go`: mensagem de `ErrContextOverflow` casa com os padrões que o
  classificador de erros existente já reconhece como não-retriable, sem
  `localllm` importar `pkg/providers` (evitaria ciclo, resolvido só no E6).
- `engine_stub.go` (build tag `!nativellm || !cgo`): todo método do engine
  devolve `ErrNotBuilt`. Esta é a única implementação compilada por padrão —
  o binding cgo real vem no E5, atrás da tag complementar — então `make build`
  continua `CGO_ENABLED=0` Go puro (BR-004/NFR-003).

## FRs cobertas

| FR | Descrição | ACs | Status |
|---|---|---|---|
| FR-002 | Render e parse ChatML/Qwen3 em Go puro | AC-002-1..5 | Verificado |

30 testes (golden files de render/parse, wiring do provider, resolução de
caminho) — confirmados existentes: `pkg/providers/localllm/chatml_test.go`,
`provider_test.go`, `resolve_test.go`.

## Commits

| Commit | Mensagem | Diff |
|---|---|---|
| `5d2fe0fe` | feat(localllm): add pure-Go ChatML rendering and provider adapter (E4) | 300 arquivos, +1065/-73868 |

**Nota sobre o diffstat**: o número de deleções é enganoso para quem procura só
"E4" — o diretório `web/` (launcher, poda do E2/ADR-007) foi removido por um
commit concorrente que sobrepôs staging na mesma branch, e a remoção acabou
carregada dentro deste commit junto com o trabalho novo do E4 (registrado
explicitamente na mensagem do commit `c9b2f9db`, que documenta essa
sobreposição). O trabalho *funcional* do E4 é só o pacote `localllm` novo
(+1065 linhas, poucos arquivos).

## Arquivos-chave

`pkg/providers/localllm/{chatml,provider,resolve,errors,engine_stub}.go` +
respectivos `_test.go`.

## Como verificar

`git show --stat 5d2fe0fe`; `go test ./pkg/providers/localllm/...` (sem tags
especiais — roda no Windows, sem cgo).

## Notas

Nenhum gap conhecido. O `engine_cgo.go` real (contraparte do `engine_stub.go`)
só chega no E5.
