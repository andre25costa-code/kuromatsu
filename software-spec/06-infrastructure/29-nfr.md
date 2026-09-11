---
id: S29
title: Requisitos não funcionais
status: confirmed
version: 2
owner: André
last_updated: 2026-09-10
depends_on: [S06, S18]
---

# S29 — Requisitos não funcionais

| ID | Categoria | Requisito | Medição |
|---|---|---|---|
| NFR-001 | Memória | Pico de RSS do container < **850 MB** (limite 900m) durante conversa de 20 turnos com o modelo nativo | `docker stats` na Oracle durante o teste do E7 |
| NFR-002 | Performance | Geração ≥ **4 tokens/s** com `n_ctx=2048` na máquina alvo (Ampere A1, 1 OCPU) | `cmd/nativebench` (3 prompts fixos, média) |
| NFR-003 | Portabilidade | `make build` padrão (CGO_ENABLED=0) compila em toda a matriz GOOS/GOARCH herdada; o stub responde `ErrNotBuilt` | `make build-all` (ou vet cruzado) no gate do E5 |
| NFR-004 | Memória (idle) | Após `keep_alive_secs` sem uso, o modelo é descarregado e o RSS cai de forma mensurável (ordem de 300+ MB liberados) | comparação de RSS antes/depois do unload no E7 |
| NFR-005 | Disponibilidade | Falha do provider nativo nunca derruba o processo: erros viram fallback/erro de turno | testes de `provider_test.go` + chain existente |

Orçamento de RAM que sustenta NFR-001 (revisado com medição real do E5, validar no E7):
modelo mmap ~231 MB + KV q8_0 @2048 (extrapolado de 29,75 MB @512) ≈ 119 MB + buffer de
computação **~150 MB (medido no E5 a n_ctx=512, não a 2048 — pode crescer com o contexto;
correção da estimativa anterior de ~60 MB)** + Go/agente 200–300 MB ⇒ pico estimado
700–800 MB, com `GOMEMLIMIT=650MiB` segurando o heap Go. Ainda dentro do limite de 900 MB
do NFR-001, mas com menos folga do que se pensava — motivo a mais para medir o buffer de
computação a `n_ctx=2048` cedo no E7, antes de fechar o `mem_limit` do compose.
