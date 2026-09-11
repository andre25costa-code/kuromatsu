---
id: S40
title: Referências
status: confirmed
version: 2
owner: André
last_updated: 2026-09-11
depends_on: []
---

# S40 — Referências

| Recurso | URL / caminho | Uso |
|---|---|---|
| PicoClaw (upstream) | https://github.com/sipeed/picoclaw | Base do fork (HEAD `bbf6893c`) |
| Fork prism do llama.cpp | https://github.com/PrismML-Eng/llama.cpp (branch `prism`, pin `d8f26eec7`) | Runtime de inferência Q1_0 |
| Coleção Bonsai (HF) | https://huggingface.co/collections/prism-ml/bonsai | Modelos |
| GGUF Bonsai-1.7B-Q1_0 | https://huggingface.co/prism-ml/Bonsai-1.7B-gguf | Fonte oficial confirmada do download (S25) |
| Bonsai-demo | repo `PrismML-Eng/Bonsai-demo` (citado no README do fork) | Binários + modelos prontos; referência |
| Exemplo de embedding | `llama.cpp/examples/simple-chat/simple-chat.cpp` | Molde do loop do `engine_cgo.go` |
| Build docs do fork | `llama.cpp/docs/build.md` | Flags de CPU (x86 AVX*/ARM dotprod-i8mm) |
| Dockerfile CPU do fork | `llama.cpp/.devops/cpu.Dockerfile` | Referência de toolchain (gcc-14/glibc) |
| Docs Clawdbot/OpenClaw | https://docs.clawd.bot | Inspiração da skill de docs (clawddocs) |

## Métricas de baseline (preencher durante a execução)

| Métrica | Antes | Depois | Registrado em |
|---|---|---|---|
| Tamanho do binário (linux/arm64, padrão) | 50 034 076 bytes (≈ 47,7 MiB) — commit `d17b2150` | 38 999 406 bytes (≈ 37,2 MiB) — commit `cb895bec` | E2 (aceite AC-007-2) |
| Pacotes Go no módulo | 110 (`go list ./...`) — commit `d17b2150` | 86 (`go list ./...`) — commit `cb895bec` | E2 |
| tok/s geração na Oracle | — | — | E7 → S39 |
| RSS pico / idle na Oracle | — | — | E7 → S39 |

Ambas as medições foram feitas com `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags goolm,stdjson`,
em git worktrees isolados presos a cada commit (evitando contaminação por trabalho em paralelo
não commitado no branch principal). "Antes" = `d17b2150` (fim do E1, ainda com `web/`, os 22 canais e
`pkg/tools/hardware`). "Depois" = `cb895bec` (fim do E2, após os 4 commits de poda: remoção do
launcher web, dos 17 canais não usados, dos tools de hardware, e de `docs/`/`examples/`). Redução de
≈ 22% no tamanho do binário e 24 pacotes (≈ 22%) a menos no módulo.
