---
id: S06
title: Premissas, restrições e riscos
status: confirmed
version: 4
owner: André
last_updated: 2026-09-11
depends_on: [S01]
---

# S06 — Premissas, restrições e riscos

## Premissas (assumidas verdadeiras, ainda não todas verificadas)

| # | Premissa | Verificação |
|---|---|---|
| A1 | ~~A instância Oracle alvo é ARM64 (Ampere A1/Neoverse N1)~~ **CORRIGIDA 2026-09-11**: André não conseguiu disponibilidade de ARM e criou a VM com shape padrão x86_64 (por isso só 1 GB de RAM — a A1 chegaria a 6 GB). Alvo real (nessa fase): **AMD EPYC 7551 "Naples" (Zen1)** — `VM.Standard.E2.1.Micro`, o shape Always Free x86 da Oracle. Zen1 tem AVX2/FMA3/F16C/BMI2, **não** tem AVX-512 nem AVX-VNNI. **Atualização 2026-09-11 (ADR-013, plano `demetrius`)**: o alvo de **produção/inferência** passa a ser a VM Google `demetrius` (`e2-micro`, Always Free do GCP) — a Oracle acima vira alvo de **backup** (S33), não roda mais inferência (`steal` 75% medido a torna inviável para isso, ver R7 abaixo) | confirmar com `lscpu` no 1º acesso à VM; smoke test no E7 (Oracle) e no Trilho 0 (`demetrius`) |
| A2 | ~4–6 tok/s de geração são alcançáveis no alvo (medido pelo usuário em VPS similar rodando só o modelo). **Atualização 2026-09-11**: confirmado na `demetrius` em condição de burst (5,1 tok/s geração, 7,3 tok/s prompt, `steal` 0) — bate NFR-002; o piso sob throttle de créditos é mais baixo (ver R7) | `nativebench`/`llama-bench` no Trilho 0 (`demetrius`) |
| A3 | O GGUF `Bonsai-1.7B-Q1_0` permanece disponível para download público | Fonte identificada e confirmada (S25 → `huggingface.co/prism-ml/Bonsai-1.7B-gguf`, ver S40); a premissa de disponibilidade *contínua* em si permanece assumida, não é verificável antecipadamente |
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
| ~~Q2~~ | ~~Política de backup da VM Oracle (snapshot? rsync do `docker/data`?)~~ **Resolvida 2026-09-11 (ADR-013, plano `demetrius`)**: a Oracle deixa de rodar inferência e vira o alvo do backup noturno cross-cloud da `demetrius` (`tar.zst` + `rsync` via SSH, timer systemd) — desenho completo em S33 (`draft`). Resolve *para onde* o backup vai; retenção de cópias e procedimento de restore continuam `tbd`, marcados como gaps explícitos no próprio S33 | André | S33 (gaps residuais: retenção, restore) |

## Riscos identificados

| # | Risco | Mitigação |
|---|---|---|
| R1 | OOM na máquina de 1 GB | ADR-003 (KV q8_0, unload); NFR-001 testado com 20 turnos |
| R2 | Modelo 1.7B gera `<tool_call>` malformado | parser tolerante com fallback `{"raw":...}` (FR-002); grammar sampling como follow-up |
| R3 | SIGILL por flag de CPU errada (AVX-512/VNNI indisponível no Zen1 do EPYC 7551) | fixar AVX2+FMA+F16C, sem AVX-512/AVX-VNNI (ADR-012); smoke test no E7 |
| R4 | Drift do fork prism quebrar o build | submodule pinado; camada Docker do cmake keyed no conteúdo do submodule |
| R5 | Rebrand quebrar deploy existente | shim de env `PICOCLAW_*` + leitura in-place de `~/.picoclaw` (ADR-005) |
| R6 | System prompt do agente estourar ctx 2048 | **Materializado no smoke test do E7 (2026-09-11)**: o prompt base (system + schema de tools do agente completo, sessão nova sem histórico) mede **~2785-2793 tokens** — sozinho já estoura `n_ctx=2048` antes de qualquer mensagem real do usuário. A pré-checagem funcionou como projetado (erro não-retriable, log com contagem real via `WarnCF`), mas a "sumarização" do pipeline não ajuda aqui: não há histórico de conversa para comprimir na primeira mensagem, o custo é o schema de tools em si. **Fecha com o plano `demetrius`** (ADR-014/015): roteador de foco (janela `chat` sem tools) + prompt/schema compactos + cache de prefixo — redesenho do perfil "enxuto" que ficava como follow-up aqui já entrou como FR-014/FR-015/FR-016 |
| R7 | **`e2-micro` (Google, alvo `demetrius`) entrega só 0,25 vCPU sustentado + créditos de burst** — um teste rápido (5 min) mede o burst e passa; se o processo gastar CPU continuamente (heartbeat/cron/sono/evolução em loop, ou uma sessão de testes pesada), os créditos se esgotam e a CPU é estrangulada (*throttled*) — a velocidade cai drasticamente sem qualquer `steal` de hipervisor aparecer no `vmstat` (diferente do problema antigo da Oracle) | **Números reais medidos nesta sessão** (`.claude/team/research/g0-medicao-real.md`): burst 5,1/7,3 tok/s (geração/prompt, medição anterior do André) vs. piso estrangulado **1,1–2,0 tok/s** (medido nesta sessão, `llama-bench`, 4 configs consistentes entre si — threads e tipo de KV testados e descartados como causa dominante). Mitigação: **CPU tratada como orçamento** — heartbeat com janela enxuta e intervalo ≥60 min (S30), preferência por jobs cron `Command` (0 tokens) sobre `Message`, sono/evolução só com modelo externo (ADR-018, preserva créditos de CPU à noite), cache de prefixo (S21/ADR-015) torna o custo por turno o *delta*, não o prompt completo, então mesmo no piso um turno fica em segundos/minuto, não nos ~6 min de hoje; bit `Throttled` (S09) e coluna `steal_pct` (S34) só informativos, sem ação automática |
| R8 | **O host físico do e2-micro varia por boot** — o André mediu um `EPYC 7B12` numa sessão anterior; nesta sessão o `lscpu` da mesma VM (`demetrius`) mostrou um `Intel Xeon @2.20GHz`. Ambos são x86_64 com AVX2/FMA/F16C e **sem** AVX-512-VNNI, mas o binário não pode assumir uma CPU física fixa | Build continua pinada em AVX2+FMA+F16C (ADR-012), sem AVX-512/AVX-VNNI — cobre os dois hosts observados; sem detecção automática de CPU em runtime (`GGML_NATIVE=OFF` continua a decisão certa) |
