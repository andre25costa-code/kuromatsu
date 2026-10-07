# Testes e suítes de integração

Fonte real: `integration/`, `AGENTS.md` (raiz — convenções de build/lint que
este arquivo não repete).

## Comandos do dia a dia

```bash
make test              # gera fontes + go test
make integration-test  # suítes Docker-backed
make check             # baixa/verifica deps, formata, vet, test — antes de submeter
```

Tags de build padrão: `CGO_ENABLED=0`, `-tags goolm,stdjson`. Preservar
essas tags em teste isolado:
`go test -tags goolm,stdjson ./pkg/session/ -run TestName -v`.

## Duas camadas de teste de integração

1. Testes Go, normalmente em `*_integration_test.go`, atrás de
   `//go:build integration`.
2. Suítes Docker-backed em `integration/suites/`, que sobem dependências
   reais e rodam um ou mais desses testes Go no CI.

A suíte Docker é o que torna o teste reproduzível e seguro pra CI; o teste
Go com a tag é a implementação em si. Alguns testes com a tag `integration`
são propositalmente opt-in e não entram em nenhuma suíte Docker (ex.:
smoke tests reais de CLI em `pkg/providers/cli/`, que dependem de binário
externo instalado localmente — úteis para verificação manual, não
bloqueiam merge de PR).

## O que o CI roda

`.github/workflows/{pr,build}.yml` executam `bash
./scripts/run-integration-tests.sh`, que autodescobre toda suíte em
`integration/suites/` — adicionar uma suíte não exige editar o workflow.

O runner compartilhado (`integration/docker-compose.runner.yml`) carrega
`suite.env` de cada suíte, mescla o compose compartilhado com o(s)
compose(s) da suíte, sobe as dependências, roda o comando, desmonta tudo.
O container do runner seta `GOFLAGS=-tags=goolm,stdjson,integration`.

## Layout de uma suíte

```
integration/suites/<nome>/
├── docker-compose.yml (ou docker-compose.*.yml)
└── suite.env
```

`suite.env` define `TEST_COMMAND` (obrigatório) e, opcionalmente,
`RUNNER_SERVICE` (default: `integration-runner`):

```bash
TEST_COMMAND='go test ./pkg/mcp -run TestIntegration_RealConfiguredServer -v'
```

Suíte de referência hoje: `integration/suites/mcp-streamable/` — builda e
sobe um servidor MCP fixture
(`integration/fixtures/mcp-streamable-server/`), injeta detalhes de conexão
via env vars no runner, e roda
`TestIntegration_RealConfiguredServer` (`pkg/mcp/manager_real_server_integration_test.go`),
que complementa `TestIntegration_StreamableHTTPCompatibility`
(`pkg/mcp/manager_integration_test.go`, roda in-process sem Docker).

## Rodando localmente

```bash
make integration-test                                    # tudo que o CI roda
bash ./scripts/run-integration-tests.sh                   # equivalente direto
bash ./scripts/run-integration-tests.sh mcp-streamable    # uma suíte só
go test -tags=goolm,stdjson,integration ./pkg/mcp -run TestIntegration_StreamableHTTPCompatibility -v  # sem Docker, iteração rápida
```

Não usar `-short` — os testes de integração atuais pulam nesse modo.

## Quando adicionar um teste de integração

Quando o risco está na **interação**, não no corpo da função isolada:
travessia de processo/container, comportamento específico de transporte
(HTTP, SSE, stdio, MCP streamable), parsing/wiring de CLI que depende de
subprocesso real, propagação de config por arquivo/env/header/descoberta de
serviço, regressão que só aparece depois que dois PRs razoáveis se juntam.
Teste unitário basta quando o comportamento é puro, local e totalmente
controlável in-process.

## Como adicionar

1. Descrever o cenário real que quebraria (quanto mais afiado, melhor a
   suíte envelhece).
2. Implementar o teste Go com `//go:build integration`; asserções focadas
   em comportamento observável, timeouts limitados, fixtures determinísticas
   em vez de acesso à internet ou estado externo compartilhado; só pular
   quando a dependência é opcional de propósito (ex.: CLI de terceiro
   instalada localmente).
3. Decidir se precisa travar merge de PR (vai numa suíte Docker) ou é só
   smoke check manual (teste com a tag basta).
4. Reusar uma suíte existente do mesmo subsistema, ou criar
   `integration/suites/<nome>/{docker-compose.yml,suite.env}` — usar
   `integration/fixtures/` para serviços/fakes reusáveis.
5. Apontar `TEST_COMMAND` no `suite.env` para o teste.
6. Validar local antes de commitar: rodar o teste direto, depois
   `bash ./scripts/run-integration-tests.sh <nome>` (prova que o caminho do
   CI funciona ponta a ponta).

## Checklist de revisão para suíte nova

Reproduz uma falha real de interação entre componentes; é determinística e
isolada; não exige setup manual no CI; tem saída de falha clara; termina em
tempo razoável; limpa depois de si via o teardown normal do runner.
