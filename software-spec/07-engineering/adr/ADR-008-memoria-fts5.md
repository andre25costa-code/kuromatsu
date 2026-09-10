---
id: ADR-008
title: Memória de longo prazo em SQLite/FTS5 (seahorse); sem ChromaDB
status: accepted
version: 1
owner: André
last_updated: 2026-09-10
depends_on: []
---

# ADR-008 — SQLite/FTS5, sem banco vetorial externo

## Contexto
O agente precisa armazenar e recuperar conhecimento com pouquíssima RAM. Candidatos:
ChromaDB (serviço externo + embeddings), banco vetorial próprio em Go, ou o
`pkg/seahorse` já existente (SQLite + FTS5/BM25, driver Go puro `modernc.org/sqlite`).

## Decisão
Usar **seahorse (SQLite FTS5/BM25)** como camada de recuperação, mais os arquivos de
memória do workspace (MEMORY.md) e sessões JSONL. **Nenhum embedding**: gerar embeddings
localmente custaria inferência extra na mesma CPU de 1 OCPU, e um serviço externo viola
a meta "sem rede". BM25 sobre texto atende recall para notas pessoais, RSS e PDFs — o
mesmo racional já validado pelo hub pessoal do usuário (FTS5-only).

## Alternativas
- ChromaDB: +1 serviço Python, +RAM, +rede — rejeitado.
- Vetor próprio em Go: interessante, mas prematuro — adiado (S10, "explicitamente adiado").

## Consequências
- Zero deps novas; o modo dormir compacta/poda direto nas tabelas do seahorse.
- Se recall de BM25 se mostrar insuficiente, reavaliar vetor nativo Go em ADR futura.
