---
id: E7
title: Pipeline de build/deploy nativo e primeiro deploy real na Oracle
status: done-with-open-gate
frs: [FR-001, FR-005]
adrs: [ADR-011, ADR-012]
commits: [8f98b336, b2912789, 4ef82aaf, bf8ee8d4, d6770194, bf52e3b8]
last_updated: 2026-09-11
---

# E7 — Deploy nativo na Oracle

## O que foi entregue

O mecanismo de build/deploy da imagem nativa (`nativellm`/cgo) está completo e
foi exercido de ponta a ponta contra a VM Oracle real. A trajetória teve uma
correção de premissa no meio, registrada abaixo com transparência porque muda
o que "entregue" significa aqui.

### 1. Pipeline original, assumindo ARM64 (`8f98b336`, ADR-011)

`.github/workflows/build-native-image.yml` (`workflow_dispatch`) compilando no
runner arm64 nativo do GitHub (`ubuntu-24.04-arm`) — decisão tomada porque a
máquina de dev (~8 GB físicos) já havia derrubado o build do E5. Build real
levou 3m05s e produziu uma imagem funcional, validada de ponta a ponta. A
premissa "a VM Oracle é ARM64 (Ampere A1)" vinha de S06/A1 e **nunca havia sido
confirmada com `lscpu`** antes de se construir sobre ela.

### 2. Correção de arquitetura (`b2912789`, `4ef82aaf`, ADR-012)

André confirmou que não conseguiu disponibilidade da shape A1 e criou a VM com
a shape x86 padrão da Oracle (`VM.Standard.E2.1.Micro`, AMD EPYC 7551
"Naples"/Zen1) — o que também explica o 1 GB de RAM (a A1 chegaria a 6 GB).
ADR-011 foi marcada `superseded_by: ADR-012` (não removida — o caminho ARM64
continua no Makefile como opção secundária/futura). A correção **simplificou**
o pipeline: build host, dev e deploy passam a ser a mesma arquitetura — sem
cross-compilação, sem QEMU. CPU pinada em `AVX2+FMA+F16C`, sem `AVX-512`/`AVX-VNNI`
(o runner de CI pode ter uma CPU mais nova que a Oracle real). Nesse mesmo
commit, S25 (fonte oficial do GGUF) foi confirmada via HEAD request
(`huggingface.co/prism-ml/Bonsai-1.7B-gguf`, ETag/tamanho batendo com o SHA256
já hardcoded desde o E1) — resolvendo a última questão TBD de S06.

### 3. Bugs reais encontrados no primeiro deploy de verdade (`bf8ee8d4`, `d6770194`, `bf52e3b8`)

Depois que a imagem corrigida foi de fato carregada (`docker load`) e rodada na
Oracle:

- **`bf8ee8d4`** — `KUROMATSU_MODELS_DIR` nunca era setada no compose, então o
  provider nativo nunca encontrava o GGUF montado em `/models`.
- **`d6770194`** — qualquer prompt não trivial (o próprio prompt do agente já
  passa de 2048 tokens) disparava `GGML_ASSERT(n_tokens_all <= cparams.n_batch)`
  e **abortava o processo** (SIGABRT, não um erro Go) porque `n_batch` estava
  ligado ao valor pequeno de `NBatch=256` enquanto `completion()` submete o
  prompt inteiro de uma vez. Corrigido separando `n_batch = n_ctx` de
  `n_ubatch` (o que de fato limita a memória do buffer de computação).
- **`bf52e3b8`** — `cmd/nativebench` (existente desde o E5) passou a ser
  embarcado na própria imagem (`/usr/local/bin/nativebench`), para permitir
  medir S39 na VM real sem rebuildar a imagem. Nesse mesmo smoke test:
  **o prompt completo do agente (system + schemas das tools, sessão nova sem
  histórico) mede ~2793 tokens** — sozinho já estourando `n_ctx=2048` antes de
  qualquer mensagem do usuário — e **o prefill desse prompt não terminou em
  15+ minutos** na VM real (1 OCPU/2 threads). Ambos os números foram medidos
  na própria VM, não estimados, e registrados em S06/R6.

## FRs cobertas

| FR | Descrição | ACs | Status |
|---|---|---|---|
| FR-005 | Build nativo reprodutível | AC-005-2, AC-005-4, AC-005-5 | Verificado (mecanismo). AC-005-6 verificado por herança do ADR-011 (caminho ARM64 mantido no Makefile, `llama-lib-arm64`/`build-native-arm64` confirmados existentes) |
| FR-001 | Inferência nativa in-process | — | Reforçado (crash fix de `d6770194`); o *gate* de desempenho (NFR-002) permanece aberto, ver abaixo |

## Gate de aceite ainda aberto — leia antes de considerar E7 "pronto para uso"

O mecanismo de build/deploy funciona e a imagem roda na Oracle sem crashar.
**O que não fecha aqui é o gate de desempenho** (NFR-002: ≥4 tok/s de geração;
S39: baseline de tok/s/RSS): o smoke test mostrou prefill impraticável na VM
real, um sintoma consistente com CPU sob throttle/steal na shape Always-Free
da Oracle, não apenas "prompt grande". Por isso:

- `S39` continua `status: tbd` em `spec-coverage.yaml` — nenhum número de
  tok/s ou RSS real na Oracle foi registrado (não inventar, ver S06/S40).
- As duas linhas correspondentes em S40 continuam vazias, agora com uma nota
  explicando por quê (ver auditoria desta tarefa em `09-appendix/40-references.md`).
- Este é exatamente o motivo pelo qual o plano de refatoração aprovado em
  2026-09-11 (fora do escopo desta auditoria — ver `BACKLOG.md`) trata a
  migração de alvo de execução e a redução do prompt/reaproveitamento de KV
  como o próximo trabalho, em vez de declarar E7 encerrado com um baseline
  ruim.

## Commits

| Commit | Mensagem | Diff |
|---|---|---|
| `8f98b336` | feat(deploy): cross-compiled native arm64 image build via GitHub Actions (E7) | 10 arquivos, +388/-25 |
| `b2912789` | fix(model): confirm the real Bonsai GGUF download source (S25) | 2 arquivos, +5/-7 |
| `4ef82aaf` | fix(deploy): correct native build target from ARM64 to x86_64 (ADR-012) | 19 arquivos, +279/-147 |
| `bf8ee8d4` | fix(deploy): set KUROMATSU_MODELS_DIR so the container finds the mounted GGUF | 1 arquivo, +4 |
| `d6770194` | fix(localllm): stop crashing on prompts over 256 tokens (n_batch vs n_ubatch) | 2 arquivos, +32/-3 |
| `bf52e3b8` | feat(deploy): ship nativebench in the native image; document the real prompt-overhead finding | 3 arquivos, +14/-2 |

## Arquivos-chave

`.github/workflows/build-native-image.yml`, `docker/Dockerfile.native`,
`Makefile` (targets `llama-lib-x86-64`, `build-native-x86-64`,
`nativebench-x86-64`, e os `-arm64` mantidos como secundários),
`docker-compose*.yml` (`KUROMATSU_MODELS_DIR`), `pkg/providers/localllm/{engine_cgo,types}.go`
(`n_batch`/`n_ubatch`), `software-spec/07-engineering/adr/{ADR-011,ADR-012}*.md`.

## Como verificar

`git show --stat <commit>` para cada linha da tabela; `grep -n "llama-lib-x86-64\|build-native-x86-64\|nativebench-x86-64" Makefile`;
`software-spec/01-business/06-assumptions.md` (R6, com o número real de
2793 tokens).

## Notas

Esta é a entrega mais "conhecimento adquirido em campo" do lote E0-E7: duas
premissas de arquitetura erradas (ARM64, orçamento de compute buffer) e dois
bugs de produção só apareceram ao rodar de verdade na Oracle, não em teste
local. Isso é tratado aqui como parte normal do entregável (cada fix tem
commit e ADR/nota associados), não como pendência — a única pendência real e
ainda aberta é o gate de desempenho (S39/NFR-002), documentado acima e no
`BACKLOG.md`.
