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
modelo realmente é (o startup e o reload já fizeram essa troca; hoje
`pkg/gateway/gateway.go` e `cmd/kuromatsu/internal/agent/helpers.go`
preservam o alias).

## O modelo nativo (Bonsai)

Entrada `provider: "native"` (aliases aceitos: `"bonsai"`, `"kuro"` — ver
`pkg/providers/common/native.go`, fonte única desses aliases, reusada tanto
pelo catálogo de providers quanto pela validação de `sleep`/`evolution`).
Roda `Bonsai-1.7B-Q1_0.gguf` in-process via cgo/llama.cpp
(`pkg/providers/localllm/`). Só existe numa build com a tag `nativellm`
(`make build-native`) **e** com o `.gguf` presente em disco — sem as duas
coisas, a entrada semeada fica desabilitada silenciosamente
(`ApplyNativeFallback`), não é erro de config.

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
provider — populado por `populateCandidateProvidersFromCandidates`.

O limite de contexto do modelo nativo como **primário** nos perfis pequenos
está em [config.md](config.md#perfis-de-hardware).

## Ollama Cloud como fallback externo

Estruturalmente é só mais uma entrada `provider: "ollama"`/openai-compat —
o cabeçalho `Authorization: Bearer <chave>` já é enviado sempre que a chave
existe, sem código dedicado. Exemplo de desenho: dois agentes em
`agents.list`, um só com o nativo (`model.fallbacks` explícito sem entrada
externa) e outro com Ollama Cloud como primário e o nativo como fallback
(decisão de projeto: o fallback externo é por agente, não global).

## 9router (roteador local)

O [9router](https://github.com/decolua/9router) é um roteador de IA local,
compatível com OpenAI, que fica na frente de dezenas de provedores. No
Kuromatsu ele é o provider `9router`, com base padrão
`http://localhost:20128/v1` (usada quando `api_base` fica vazio).

- **Chave opcional.** O 9router vem com `REQUIRE_API_KEY=false`. Sem chave,
  o Kuromatsu não envia `Authorization`. Configure uma chave só se o
  9router estiver com `REQUIRE_API_KEY=true`; ela vai como
  `Authorization: Bearer <chave>`.
- **ID literal.** Com `provider: "9router"`, o `model` chega como está no
  corpo da requisição: IDs com prefixo (`cc/claude-opus-4-7`,
  `kr/claude-sonnet-4.5`, `glm/glm-5.1`, `minimax/MiniMax-M2`) e nomes de
  combo (`premium-coding`). O prefixo escolhe o provedor dentro do 9router,
  então o Kuromatsu não o corta.

Três formas de configurar:

1. `kuromatsu onboard`: opção "9router (roteador local)". Enter na base usa
   a padrão; Enter na chave grava a entrada sem chave.
2. Web UI, tela Modelos: provedor `9router`. Base em branco usa a padrão no
   "Testar conexão" e na entrada salva.
3. `kuromatsu model add --type 9router` (lista os modelos e pede um) ou
   `kuromatsu model add --type 9router -m cc/claude-opus-4-7 -n router`.
   `-b` e `-k` são opcionais.

As três gravam `provider: "9router"`. Ao escrever a config à mão, o
`provider` é **obrigatório**:

```json
{
  "model_list": [
    {
      "model_name": "router-opus",
      "provider": "9router",
      "model": "cc/claude-opus-4-7",
      "api_base": "http://localhost:20128/v1"
    },
    {
      "model_name": "router-glm",
      "provider": "9router",
      "model": "glm/glm-5.1"
    }
  ],
  "agents": {
    "defaults": {
      "model_name": "router-opus",
      "model_fallbacks": ["router-glm"]
    }
  }
}
```

Sem `provider`, o prefixo é lido como provedor do catálogo: `glm/glm-5.1`
vira Zhipu e `minimax/...` vira MiniMax (e perde o prefixo no envio).

**Referencie pelo alias.** Em `model_fallbacks` e em `agents.list[].model`,
use o `model_name` da entrada (`router-glm`), nunca o ID cru: uma
referência `glm/glm-5.1` não passa pela entrada do 9router e vai para a
Zhipu.

**Tool calling: não verificado.** O 9router traduz formatos entre OpenAI,
Claude e Gemini, mas não promete suporte a tools; depende do provedor por
trás do prefixo. Para conferir:

1. O teste de conexão (onboard ou UI, `GET /v1/models`) prova só a conexão
   e a lista de modelos.
2. Com o modelo do 9router como default, rode
   `kuromatsu agent -m "Liste os arquivos do workspace usando a ferramenta de listar diretório."`
   e confira na resposta ou no log que houve uma chamada de tool, e não um
   texto descrevendo uma.
3. Se falhar, troque de prefixo ou de combo no 9router, ou use esse modelo
   só como fallback.

Algumas heurísticas por nome de modelo continuam valendo (IDs com `glm`,
`o1` ou `gpt-5` enviam `max_completion_tokens`); se o provedor por trás
recusar o campo, fixe `max_tokens_field` na entrada.

**Deploy.** O 9router é um app Node.js; não rode ao lado do Kuromatsu numa
VM pequena (1 GB). Rode-o em outra máquina ou localmente e aponte o
`api_base` para ele:

- mesma máquina: a base padrão;
- remoto: `http://<your-host>:20128/v1`, ou de preferência um túnel SSH até
  o loopback do 9router.

Fora do loopback, ligue `REQUIRE_API_KEY=true` no 9router e configure a
chave no Kuromatsu: o 9router guarda as credenciais OAuth dos outros
provedores, e quem alcança a porta usa as suas contas.

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
