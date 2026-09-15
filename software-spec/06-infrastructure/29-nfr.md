---
id: S29
title: Requisitos não funcionais
status: confirmed
version: 4
owner: André
last_updated: 2026-09-11
depends_on: [S06, S18, S21]
---

# S29 — Requisitos não funcionais

| ID | Categoria | Requisito | Medição |
|---|---|---|---|
| NFR-001 | Memória | Pico de RSS do container < **850 MB** (limite 900m) durante conversa de 20 turnos com o modelo nativo | `docker stats` na Oracle durante o teste do E7 |
| NFR-002 | Performance | Geração ≥ **4 tokens/s**, alvo agora a VM Google `demetrius` (`e2-micro`, ADR-013 — CPU física varia por boot, ver S06/A1/R8), **desdobrada em burst × piso** (ver nota abaixo) | `cmd/nativebench` (3 prompts fixos, média) + `.claude/team/research/g0-medicao-real.md` |
| NFR-003 | Portabilidade | `make build` padrão (CGO_ENABLED=0) compila em toda a matriz GOOS/GOARCH herdada; o stub responde `ErrNotBuilt` | `make build-all` (ou vet cruzado) no gate do E5 |
| NFR-004 | Memória (idle) | Após `keep_alive_secs` sem uso, o modelo é descarregado e o RSS cai de forma mensurável (ordem de 300+ MB liberados) | comparação de RSS antes/depois do unload no E7 |
| NFR-005 | Disponibilidade | Falha do provider nativo nunca derruba o processo: erros viram fallback/erro de turno | testes de `provider_test.go` + chain existente |
| NFR-006 | Performance | Turno típico (janela de foco resolvida, cache de prefixo quente) ≤ **90 s** em condição de burst | Telemetria por turno (`total_ms`, S34) — meta do gate G2 (Trilho A); ainda não medido em produção (A/B pendentes) |
| NFR-007 | Performance | Overhead de framework por janela ≤ **800 tokens** de prompt (`chat` ≤450, `heartbeat` ≤300), medido separadamente do texto do workspace do usuário (AGENT/SOUL/USER/MEMORY) | `prompt_size_test` (A9) — meta de design (ADR-014), ainda não medido; workspace real do usuário (~1,0–1,2k tokens PT-BR) é custo aditivo do usuário, não do framework |
| NFR-008 | Memória | `MemAvailable` idle ≥ **250 MB** *com o modelo carregado* (mantido por `keep_alive_secs: -1`) | Ver nota de medição parcial abaixo — **ainda não medido nessa condição exata** |
| NFR-009 | Performance | Tokens em cache (`cached_tokens/prompt_tokens`) ≥ **70%** a partir do 2º turno na mesma janela de foco | Telemetria por turno (`cached_tokens`, S21/S34) — meta do gate G1 (Trilho B), ainda não medido (B0/B1 pendentes) |

Orçamento de RAM que sustenta NFR-001 (revisado com medição real do E5, validar no E7):
modelo mmap ~231 MB + KV q8_0 @2048 (extrapolado de 29,75 MB @512) ≈ 119 MB + buffer de
computação **~150 MB (medido no E5 a n_ctx=512, não a 2048 — pode crescer com o contexto;
correção da estimativa anterior de ~60 MB)** + Go/agente 200–300 MB ⇒ pico estimado
700–800 MB, com `GOMEMLIMIT=650MiB` segurando o heap Go. Ainda dentro do limite de 900 MB
do NFR-001, mas com menos folga do que se pensava — motivo a mais para medir o buffer de
computação a `n_ctx=2048` cedo no E7, antes de fechar o `mem_limit` do compose.

## NFR-002 desdobrada: burst × piso estrangulado (2026-09-11)

O e2-micro do Google usa um modelo de *bursting*: 0,25 vCPU sustentado + créditos de
burst acumulados. O gate formal de NFR-002 (≥4 tok/s) é medido **em burst** — é isso
que o André mediu antes desta sessão e é o número que valida a migração
(`demetrius` bate a meta; a Oracle, com `steal` 75%, nunca bateria). O **piso**
(créditos esgotados) é registrado como informativo, não como um segundo gate —
porque o Trilho B (cache de prefixo, S21) torna o custo por turno o *delta*, não o
prompt completo, e mesmo no piso um delta de 50–300 tokens processa em segundos, não
minutos.

| Condição | pp (tok/s) | tg (tok/s) | Origem |
|---|---|---|---|
| Burst (créditos disponíveis) | 7,3 | 5,1 | Medição do André, `llama-cli`, antes desta sessão — **não** remedida por nós ainda numa VM "fria" |
| Piso (créditos esgotados) | 1,6–2,0 | 1,1–1,3 | Medido nesta sessão, `llama-bench`, 4 configs (threads 2/4 × KV q8_0/f16) — resultado consistente entre as 4, ~4× mais lento que o burst |

**Não inventar um piso "sustentado de longo prazo" único ainda** — o número acima é
o vale medido *imediatamente após* ~30–40 min de CPU quase contínua na sessão (duas
tentativas de `nativebench`, `dd` de disco, múltiplas sessões SSH); pode ser pior que
o platô real de longo prazo. Recomendação registrada em
`.claude/team/research/g0-medicao-real.md`: remedir numa sessão futura, com a VM
"fria", antes de fechar o baseline definitivo em S39.

## NFR-008: nota de medição parcial

`MemAvailable` idle **sem o modelo carregado**, na `demetrius` já trimada (Trilho 0,
Ops Agent/osconfig/exim4/rsyslog/haveged/Docker desativados + sysctl aplicado):
**696 MB** medidos nesta sessão (vs. 466 MB antes do trim) — bate a meta do gate G0
(≥650 MB) com folga. Isso **não é** a mesma condição do NFR-008 (que pede o piso
*com o modelo carregado e mantido quente*, `keep_alive_secs: -1`): subtraindo a
estimativa de modelo+KV+buffer de computação (~231+119+150 MB, ver orçamento de RAM
acima) dos 696 MB medidos, a folga projetada fica bem acima de 250 MB, mas isso é
uma **extrapolação**, não uma medição direta da condição do NFR-008 — falta medir
`MemAvailable` com o processo real rodando e o modelo carregado por um período
prolongado na `demetrius` (fica para o gate G3, junto do memguard/C3).
