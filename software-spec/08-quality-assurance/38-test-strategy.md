---
id: S38
title: Estratégia de teste
status: confirmed
version: 1
owner: André
last_updated: 2026-09-21
depends_on: [S11]
---

# S38 — Estratégia de teste

> **Status**: `confirmed`. Estava `missing` com a nota "estratégia de
> teste está distribuída nos gates de cada entregável (S11)... consolidar
> se crescer." Cresceu: o audit de 2026-09-16
> (`docs/internal-audit-2026-09-16.md`) rodou a suíte inteira com `-race`
> em pacotes-alvo, testes de integração com GGUF real, e lint incremental
> como gate formal antes de qualquer deploy — volume e disciplina
> suficientes para um capítulo próprio, consolidando o que já existia
> disperso.

## Camadas de teste, do mais rápido ao mais caro

1. **Unitário** (`go test ./...`, `CGO_ENABLED=0`, tags `goolm,stdjson`) —
   a maioria dos ~798 arquivos Go de teste do repo. Roda em segundos,
   sempre, em toda mudança.
2. **`-race`** — não roda por padrão em todo `go test`; usado
   deliberadamente nos pacotes onde concorrência é o risco real
   (`pkg/seahorse`, `pkg/tools`, `pkg/sleep`, `pkg/config` no audit de
   2026-09-16 — pegou um caso real de double-checked-locking invertido em
   `pkg/seahorse/short_engine.go`, CONC-01). Rodar `-race` num pacote
   específico quando a mudança toca estado compartilhado entre goroutines,
   não em todo commit.
3. **Integração** (`//go:build integration`, suítes Docker em
   `integration/suites/`) — travessia de processo/container, transporte
   real, CLI real. Ver `workspace/skills/kuromatsu-docs/references/testing.md`
   para como adicionar uma.
4. **Integração nativa (cgo/GGUF real)** — opt-in
   (`KUROMATSU_INTEGRATION_TESTS=1` + `KUROMATSU_TEST_MODEL=<caminho do
   .gguf>`), tag `nativellm`. Único jeito de validar o engine cgo de
   verdade: cache de prefixo, núcleos de janela (B2), cancelamento durante
   prefill, overflow real, reload guard. Não roda no CI padrão (precisa do
   `.gguf` de ~237MB); roda manualmente antes de qualquer deploy que toque
   `pkg/providers/localllm/`.
5. **Lint** (`golangci-lint`, `--build-tags goolm,stdjson[,nativellm]`) —
   modo incremental (`--new-from-rev HEAD`) no dia a dia; não é
   certificação de zero dívida em todo o histórico, só de que a mudança
   atual não introduz ocorrência nova.

## Testes golden e "número real, não estimativa"

Dois padrões recorrentes valem nomear porque aparecem em várias entregas:

- **Teste golden** de schema de tool/prompt (ex.:
  `pkg/providers/common/compact_schema_test.go`) — compara a saída real
  contra um valor fixo esperado, pega regressão de formato sem precisar
  rodar um modelo de verdade.
- **Medição real embutida no teste** (não estimativa) — quando uma PR
  alega "isso é mais eficiente", o teste mede e loga o número real em vez
  de assumir (ex.: `pkg/agent/focus_window_overhead_test.go` mede tokens de
  overhead por janela de foco com um workspace vazio real; um miss de
  orçamento é logado, não escondido, para dar visibilidade sem travar o
  build por um alvo nunca antes medido). Esse padrão é o critério de
  aceite institucional citado no plano de refatoração (O4/KR4.1) e deve
  ser reusado sempre que uma alegação de desempenho entrar num PR.

## O que os gates de CI cobrem, e o que não cobrem

`make check` (baixar/verificar deps, formatar, `vet`, `test`) é o piso
antes de qualquer PR. CI (`.github/workflows/{pr,build}.yml`) roda isso
mais as suítes de integração Docker-backed. **Não cobrem**: o caminho
`nativellm`/cgo (precisa do `.gguf`, roda manual), o comportamento real em
produção (ver `deploy.md` — verificação pós-deploy é uma checagem
separada, não substituível por teste local), e — achado explícito do audit
de 2026-09-16 — a suíte Docker completa às vezes não roda no ambiente do
próprio desenvolvedor (WSL sem Docker Desktop, por exemplo); nesse caso o
teste Go com a tag `integration` direto (sem Docker) é o substituto válido
para validação manual, não para gate de CI.

## Regressão pré-existente conhecida (não é falha introduzida por uma PR)

`TestAgentLoop_Run_AutoContinuesLateSteeringMessage`
(`pkg/agent/steering_test.go`) é intermitente **só sob carga de suíte
completa em paralelo** (race real entre uma goroutine de entrega tardia de
steering e o fim do turno principal) — 5/5 limpo isolado, confirmado em
mais de uma rodada nesta sessão. Antes de tratar uma falha nesse teste
específico como regressão de uma mudança, rodá-lo isolado
(`-run '^TestAgentLoop_Run_AutoContinuesLateSteeringMessage$'`) algumas
vezes primeiro.

## Cross-referências

- **Como rodar/adicionar suíte de integração**:
  `workspace/skills/kuromatsu-docs/references/testing.md`.
- **Convenções de build/lint/commit**: `AGENTS.md` (S36).
- **Origem do padrão "número real, não estimativa"**: plano de
  refatoração, Trilho F, O4/KR4.1 (`C:\Users\pc\.claude\plans\quero-refatorar-esse-projeto-twinkly-papert.md`).
- **Achados de concorrência e correção real encontrados por esta
  disciplina**: `docs/internal-audit-2026-09-16.md`.
