---
id: S40
title: Referências
status: confirmed
version: 4
owner: André
last_updated: 2026-09-13
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
| tok/s geração na Oracle | — | — | Alvo mudou para `demetrius` (ADR-013) — Oracle não roda mais inferência; linha fica vazia por desenho, não por pendência |
| RSS pico / idle na Oracle | — | — | Idem — Oracle é alvo de backup (ADR-013), não de inferência |
| tok/s geração na `demetrius` (piso, créditos exauridos) | — | 0,44–1,7 (múltiplas medições, 2026-09-13) | S39 |
| RSS pico / idle na `demetrius` (modelo carregado) | 87,5 MB idle | 375–590 MB com modelo ativo | S39 |
| Cache de prefixo do KV — hit % real (dentro do turno / entre turnos de heartbeat) | 0% (turno frio) | 98,3% / 88,0% | S39 |

> **Por que as duas primeiras linhas (Oracle) continuam vazias (nota
> 2026-09-11, auditoria E0-E7, reconfirmada em 2026-09-13)**: não é uma
> pendência esquecida — o smoke test real de E7 na Oracle (commit
> `bf52e3b8`) mediu que o prefill do prompt completo do agente (~2793
> tokens, registrado em S06/R6) não terminava em 15+ minutos na VM real
> (`VM.Standard.E2.1.Micro`, 1 OCPU, `steal` 75%). Um número de tok/s
> extraído desse cenário não serviria de baseline. A migração de alvo para
> `demetrius` (ADR-013) tornou essa medição definitivamente fora de
> escopo — as linhas ficam vazias por desenho. As três linhas novas abaixo
> (`demetrius`) têm a medição real detalhada em S39; o piso "burst pleno"
> em VM fria ainda não foi medido (ver S39, "o que não foi medido").

Ambas as medições de binário/pacotes acima foram feitas com `GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags goolm,stdjson`,
em git worktrees isolados presos a cada commit (evitando contaminação por trabalho em paralelo
não commitado no branch principal). "Antes" = `d17b2150` (fim do E1, ainda com `web/`, os 22 canais e
`pkg/tools/hardware`). "Depois" = `cb895bec` (fim do E2, após os 4 commits de poda: remoção do
launcher web, dos 17 canais não usados, dos tools de hardware, e de `docs/`/`examples/`). Redução de
≈ 22% no tamanho do binário e 24 pacotes (≈ 22%) a menos no módulo.
