---
id: S40
title: Referências
status: confirmed
version: 1
owner: André
last_updated: 2026-09-10
depends_on: []
---

# S40 — Referências

| Recurso | URL / caminho | Uso |
|---|---|---|
| PicoClaw (upstream) | https://github.com/sipeed/picoclaw | Base do fork (HEAD `bbf6893c`) |
| Fork prism do llama.cpp | https://github.com/PrismML-Eng/llama.cpp (branch `prism`, pin `d8f26eec7`) | Runtime de inferência Q1_0 |
| Coleção Bonsai (HF) | https://huggingface.co/collections/prism-ml/bonsai | Modelos; candidata a fonte do download (S25) |
| Bonsai-demo | repo `PrismML-Eng/Bonsai-demo` (citado no README do fork) | Binários + modelos prontos; referência |
| Exemplo de embedding | `llama.cpp/examples/simple-chat/simple-chat.cpp` | Molde do loop do `engine_cgo.go` |
| Build docs do fork | `llama.cpp/docs/build.md` | Flags ARM64 (dotprod/i8mm) |
| Dockerfile CPU do fork | `llama.cpp/.devops/cpu.Dockerfile` | Referência de toolchain (gcc-14/glibc) |
| Docs Clawdbot/OpenClaw | https://docs.clawd.bot | Inspiração da skill de docs (clawddocs) |

## Métricas de baseline (preencher durante a execução)

| Métrica | Antes | Depois | Registrado em |
|---|---|---|---|
| Tamanho do binário (linux/arm64, padrão) | — | — | E2 (aceite AC-007-2) |
| Pacotes Go no módulo | — | — | E2 |
| tok/s geração na Oracle | — | — | E7 → S39 |
| RSS pico / idle na Oracle | — | — | E7 → S39 |
