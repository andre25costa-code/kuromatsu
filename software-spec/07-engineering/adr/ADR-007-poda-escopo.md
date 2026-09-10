---
id: ADR-007
title: Poda de escopo (web/, canais, hardware) antes do rebrand
status: accepted
version: 1
owner: André
last_updated: 2026-09-10
depends_on: []
---

# ADR-007 — Poda antes do rebrand

## Contexto
O fork herda ~273k linhas Go: um launcher web (Go+React), 22 canais de chat, tools de
hardware para boards Sipeed. O usuário usa CLI/Docker + Telegram/WhatsApp. O rebrand
(module path em ~490 arquivos) fica menor e mais barato se vier depois da poda.

## Decisão
Remover, em commits independentes que compilam: `web/` inteiro; canais exceto `pico`,
`pico_client`, `telegram`, `whatsapp`, `whatsapp_native`; `pkg/tools/hardware`
(i2c/spi/serial) e o canal `maixcam`; `examples/`, integração órfã e `docs/` legado.
Os tools de hardware são substituídos funcionalmente pelo `sysmon` (FR-011) — foco em
observabilidade de memória/processos na máquina ARM64, não em GPIO.

## Alternativas
- Manter tudo e só rebrandear — mantido como opção rejeitada pelo usuário (mais
  manutenção, binário maior, rename mais caro).

## Consequências
- `go mod tidy` remove deps pesadas (mautrix, whatsmeow fica, etc. conforme canal).
- Registrar tamanho de binário antes/depois (S40). Código removido continua acessível
  no histórico git/upstream — nada é "perdido".
