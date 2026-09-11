---
id: S06
title: Premissas, restrições e riscos
status: confirmed
version: 2
owner: André
last_updated: 2026-09-11
depends_on: [S01]
---

# S06 — Premissas, restrições e riscos

## Premissas (assumidas verdadeiras, ainda não todas verificadas)

| # | Premissa | Verificação |
|---|---|---|
| A1 | ~~A instância Oracle alvo é ARM64 (Ampere A1/Neoverse N1)~~ **CORRIGIDA 2026-09-11**: André não conseguiu disponibilidade de ARM e criou a VM com shape padrão x86_64 (por isso só 1 GB de RAM — a A1 chegaria a 6 GB). Alvo real: **AMD EPYC 7551 "Naples" (Zen1)** — `VM.Standard.E2.1.Micro`, o shape Always Free x86 da Oracle. Zen1 tem AVX2/FMA3/F16C/BMI2, **não** tem AVX-512 nem AVX-VNNI | confirmar com `lscpu` no 1º acesso à VM; smoke test no E7 |
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
| C7 | Máquina de desenvolvimento é Windows 11 (WSL, ~8 GB RAM física — insuficiente para o build de ggml/llama.cpp, ver E5); builds cgo linux/amd64 saem do runner nativo do GitHub Actions | estratégia de build do E5/E7, ADR-011/012 |

## Questões abertas (TBD)

| # | Questão | Quem decide | Bloqueia |
|---|---|---|---|
| ~~Q1~~ | ~~URL/fonte oficial de download do GGUF~~ **Resolvida 2026-09-11**: `huggingface.co/prism-ml/Bonsai-1.7B-gguf` (S25) | André | — |
| Q2 | Política de backup da VM Oracle (snapshot? rsync do `docker/data`?) | André | go-live do E7 (S33) |

## Riscos identificados

| # | Risco | Mitigação |
|---|---|---|
| R1 | OOM na máquina de 1 GB | ADR-003 (KV q8_0, unload); NFR-001 testado com 20 turnos |
| R2 | Modelo 1.7B gera `<tool_call>` malformado | parser tolerante com fallback `{"raw":...}` (FR-002); grammar sampling como follow-up |
| R3 | SIGILL por flag de CPU errada (AVX-512/VNNI indisponível no Zen1 do EPYC 7551) | fixar AVX2+FMA+F16C, sem AVX-512/AVX-VNNI (ADR-012); smoke test no E7 |
| R4 | Drift do fork prism quebrar o build | submodule pinado; camada Docker do cmake keyed no conteúdo do submodule |
| R5 | Rebrand quebrar deploy existente | shim de env `PICOCLAW_*` + leitura in-place de `~/.picoclaw` (ADR-005) |
| R6 | System prompt do agente estourar ctx 2048 | **Materializado no smoke test do E7 (2026-09-11)**: o prompt base (system + schema de tools do agente completo, sessão nova sem histórico) mede **~2785-2793 tokens** — sozinho já estoura `n_ctx=2048` antes de qualquer mensagem real do usuário. A pré-checagem funcionou como projetado (erro não-retriable, log com contagem real via `WarnCF`), mas a "sumarização" do pipeline não ajuda aqui: não há histórico de conversa para comprimir na primeira mensagem, o custo é o schema de tools em si. Mitigação real: `n_ctx` operacional subiu (valor final registrado em S18 após o teste de vazão no E7); redesenhar um perfil de tools "enxuto" para o modelo nativo fica como possível follow-up (não implementado agora) |
