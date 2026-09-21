# Modelos e providers

Fonte real: `pkg/providers/`, `pkg/config/{native_fallback,defaults}.go`,
`pkg/agent/instance.go`.

## `model_list` — a fonte de verdade

Cada entrada de `model_list` tem `model_name` (alias interno usado em todo
o resto da config), `provider`, `model` (ID físico no provider),
`api_base`, `extra_body`, `tool_schema_transform`, etc. A chave de API mora
em `.security.yml`, indexada pelo `model_name`, nunca aqui.

**Não perder o alias**: código que resolve um provider a partir de
`model_list` deve manter o `model_name` configurado, não sobrescrevê-lo
pelo ID físico devolvido pela fábrica do provider — fazer isso faz
`/show model` e a resolução seguinte perderem de vista qual provider aquele
modelo realmente é (achado real do audit, CFG-01/CFG-03: startup e reload
faziam exatamente essa troca; corrigido em `pkg/gateway/gateway.go` e
`cmd/kuromatsu/internal/agent/helpers.go` — preservar o alias).

## O modelo nativo (Bonsai)

Entrada `provider: "native"` (aliases aceitos: `"bonsai"`, `"kuro"` — ver
`pkg/providers/common/native.go`, fonte única desses aliases, reusada tanto
pelo catálogo de providers quanto pela validação de `sleep`/`evolution`).
Roda `Bonsai-1.7B-Q1_0.gguf` in-process via cgo/llama.cpp
(`pkg/providers/localllm/`). Só existe numa build com a tag `nativellm`
(`make build-native`) **e** com o `.gguf` presente em disco — sem as duas
coisas, a entrada semeada fica desabilitada silenciosamente
(`ApplyNativeFallback`, BR-002), não é erro de config.

`ApplyNativeFallback` decide o papel do nativo nos **defaults globais**
(`cfg.Agents.Defaults`): sem nenhuma chave de API configurada em lugar
nenhum, o nativo se torna o modelo default; com uma chave configurada, o
nativo entra como último item de `model_fallbacks` em vez de assumir o
default. Isso só toca `cfg.Agents.Defaults.ModelFallbacks` — nunca um
`agents.list[].model.fallbacks` explícito (ver abaixo).

## Fallback entre modelos/providers

`resolveAgentModel`/`resolveAgentFallbacks` (`pkg/agent/instance.go`)
decidem o candidato primário e a cadeia de fallback por agente:
`agents.list[].model.primary`/`.fallbacks`, se setados, **sobrescrevem
inteiramente** os defaults globais — inclusive excluindo o nativo, mesmo
que `ApplyNativeFallback` o tenha injetado nos defaults. É assim que um
agente "só ollama, sem nativo nenhum" já é alcançável hoje sem código novo:
basta um `fallbacks` explícito (mesmo curto) na config do agente.

`AgentInstance.CandidateProviders` mantém um `providers.LLMProvider` vivo
por candidato (chave = `candidateProviderKey`), permitindo cadeia de
fallback **entre providers diferentes**, não só entre modelos do mesmo
provider — populado por `populateCandidateProvidersFromNames`/
`...FromCandidates`.

## Ollama Cloud como fallback externo

Estruturalmente é só mais uma entrada `provider: "ollama"`/openai-compat —
o cabeçalho `Authorization: Bearer <chave>` já é enviado sempre que a chave
existe, sem código dedicado. Ver ADR-019 para o exemplo completo de config
(dois agentes, um só-nativo, outro com Ollama Cloud + nativo como
fallback).

## Schema de tools compacto (`tool_schema_transform: "compact"`)

Reduz descrição de tool a uma frase e limita enum/propriedades aninhadas —
pensado para modelos pequenos/locais onde cada token de overhead de
framework custa caro. Ligado por padrão na entrada nativa semeada
(`pkg/config/defaults.go`); qualquer `model_list` pode ativar o mesmo campo.

## Erros e retry entre chamadas

`pkg/providers/error_classifier.go` classifica erro de provider em razões
(`rate_limit`, `timeout`, `server_error`, `network`, `context_length_exceeded`,
...) que `pipeline_llm.go` usa pra decidir retry com backoff
(`agents.defaults.max_llm_retries`/`llm_retry_backoff_secs`) versus
compressão de contexto versus desistir. Suspensão do `runstate` (memguard
sob pressão de memória) tem orçamento de espera **próprio**
(`runstate_resume_wait_secs`), separado do backoff genérico — ver
`memory-management.md`.
