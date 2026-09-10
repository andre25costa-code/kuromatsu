---
id: S06
title: Premissas, restrições e riscos
status: confirmed
version: 1
owner: André
last_updated: 2026-09-10
depends_on: [S01]
---

# S06 — Premissas, restrições e riscos

## Premissas (assumidas verdadeiras, ainda não todas verificadas)

| # | Premissa | Verificação |
|---|---|---|
| A1 | A instância Oracle alvo é ARM64 (Ampere A1 / Neoverse N1): tem NEON **dotprod**, **não** tem i8mm | smoke test no 1º deploy (E7); `lscpu` na máquina |
| A2 | ~4–6 tok/s de geração são alcançáveis no alvo (medido pelo usuário em VPS similar rodando só o modelo) | `nativebench` no E7 |
| A3 | O GGUF `Bonsai-1.7B-Q1_0` permanece disponível para download público | resolver S25 (fonte oficial) |
| A4 | O template de chat embutido no GGUF (ChatML/Qwen3 com `# Tools`) é estável entre releases do modelo | golden tests do ChatML (FR-002) |
| A5 | O backup do hub pessoal em `docker/data/workspace/` está completo antes de substituir `workspace/` | conferido na exploração; re-verificar no E1 antes de sobrescrever |

## Restrições (não negociáveis)

| # | Restrição | Consequência |
|---|---|---|
| C1 | 1 GB de RAM total na máquina de produção | KV q8_0, ctx 2048, single-flight, keep-alive unload (ADR-003); `GOMEMLIMIT`/`mem_limit` |
| C2 | GitHub bloqueia arquivos > 100 MB | GGUF (237 MB) fora do git; script de download (ADR-009) |
| C3 | O tipo `Q1_0` só existe no fork prism — **não misturar** com llama.cpp stock | submodule pinado em `d8f26eec7` (ADR-009) |
| C4 | Build padrão do projeto deve permanecer `CGO_ENABLED=0` e compilar em toda a matriz GOOS/GOARCH existente | build tag `nativellm` + stub (ADR-001, NFR-003, BR-004) |
| C5 | Dados pessoais (hub, sessões, segredos) jamais no repo público ou embutidos no binário | BR-003; template de workspace no E1 |
| C6 | Documentação nova em PT-BR | decisão do usuário |
| C7 | Máquina de desenvolvimento é Windows 11; builds cgo linux/arm64 saem via Docker | estratégia de build do E5/E7 |

## Questões abertas (TBD)

| # | Questão | Quem decide | Bloqueia |
|---|---|---|---|
| Q1 | URL/fonte oficial de download do GGUF (HF `prism-ml/bonsai`? release `Bonsai-demo`?) | André | script do E1 (FR-012); S25 |
| Q2 | Política de backup da VM Oracle (snapshot? rsync do `docker/data`?) | André | go-live do E7 (S33) |

## Riscos identificados

| # | Risco | Mitigação |
|---|---|---|
| R1 | OOM na máquina de 1 GB | ADR-003 (KV q8_0, unload); NFR-001 testado com 20 turnos |
| R2 | Modelo 1.7B gera `<tool_call>` malformado | parser tolerante com fallback `{"raw":...}` (FR-002); grammar sampling como follow-up |
| R3 | SIGILL por flag de CPU errada (i8mm no Ampere) | fixar `armv8.2-a+dotprod+fp16` (E5/E7); smoke test |
| R4 | Drift do fork prism quebrar o build | submodule pinado; camada Docker do cmake keyed no conteúdo do submodule |
| R5 | Rebrand quebrar deploy existente | shim de env `PICOCLAW_*` + leitura in-place de `~/.picoclaw` (ADR-005) |
| R6 | System prompt do agente estourar ctx 2048 | pré-checagem → overflow não-retriable → sumarização existente do pipeline |
