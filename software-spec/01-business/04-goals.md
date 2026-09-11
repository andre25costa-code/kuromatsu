---
id: S04
title: Objetivos do projeto
status: confirmed
version: 2
owner: André
last_updated: 2026-09-11
depends_on: [S01]
---

# S04 — Objetivos do projeto

| Camada | Objetivo | Como medir |
|---|---|---|
| Negócio | Assistente pessoal 24/7 com custo marginal zero (sem chave de API obrigatória) | Agente responde com `config.json` vazio de chaves (AC-003-1) |
| Produto | Rodar a stack completa em Docker numa Oracle x86_64 (EPYC 7551) com 1 GB de RAM | NFR-001 (pico < 850 MB), deploy do E7 no ar |
| Produto | Manter compatibilidade com modelos externos via chaves (API na frente, nativo como fallback) | AC-003-2 |
| Técnico | Inferência nativa in-process a ≥ 4 tokens/s de geração na máquina alvo | NFR-002 via `nativebench` |
| Técnico | Build padrão continua Go puro e multiplataforma | NFR-003 |
| Técnico | Repositório publicável no GitHub sem dados pessoais nem arquivos > 100 MB | BR-003, aceite do E1/E3 |
