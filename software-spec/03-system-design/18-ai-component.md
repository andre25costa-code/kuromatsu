---
id: S18
title: Componente de IA — Bonsai-1.7B-Q1_0 embutido
status: confirmed
version: 1
owner: André
last_updated: 2026-09-10
depends_on: [S06, S13]
---

# S18 — Componente de IA

## Modelo e runtime

| Aspecto | Valor |
|---|---|
| Modelo | `Bonsai-1.7B-Q1_0.gguf` — 237 MB, arch **qwen3** (28 camadas, 8 KV heads, head dim 128), tokenizer BPE Qwen2, 151.669 tokens |
| Quantização | `Q1_0` (1.125 bpw): 1 bit de sinal/peso + escala fp16 por bloco de 128 — só existe no fork prism |
| Runtime | `libllama`/`libggml` do fork `PrismML-Eng/llama.cpp` @ `d8f26eec7`, CPU-only, estático (ADR-001/009) |
| Kernels ARM64 | NEON `sdot` (`ggml_vec_dot_q1_0_q8_0`) + repack GEMM `q1_0_4x4` via dotprod — build fixado em `armv8.2-a+dotprod+fp16` (sem i8mm; ver A1/R3) |
| Contexto | GGUF suporta 32k (YaRN); **operacional: `n_ctx=2048`** por orçamento de RAM (C1) |
| KV cache | `q8_0` (≈ 59 KB/token vs 112 KB f16) — default via `ExtraBody.kv_cache_type` |
| Sampler | Defaults gravados no GGUF: `top_k=20`, `top_p=0.85`, `temp=0.5` — respeitados como default do provider |

## Template de chat e tool-calling

O `tokenizer.chat_template` do GGUF é ChatML/Qwen3 com bloco `# Tools`. Como
`llama_chat_apply_template` não suporta tools, o render é feito **em Go**
(`chatml.go`, ADR-004, FR-002):

- system + `# Tools` + `<tools>` (um JSON de função por linha) + instrução `<tool_call>`;
- resultados de tool viram `<tool_response>` em turno `user` (consecutivos fundidos);
- prefill `<think>\n\n</think>\n\n` no turno assistant final desliga o gasto de thinking
  do 1.7B (configurável);
- parse tolerante da saída: `<think>` → reasoning; `<tool_call>` JSON → calls (malformado
  → `{"raw": ...}`); máximo 4 calls por resposta.

## Estratégia multi-modelo

Prioridade da cadeia (FR-003): modelos com chave de API primeiro; `bonsai-local` como
fallback permanente; sem chave nenhuma, o local é o padrão. Escalação a humano: n/a —
o "humano" é o próprio usuário conversando.

## Guardrails

- Overflow de contexto detectado **antes** do decode (prompt + 64 > n_ctx) → erro
  não-retriable → sumarização do pipeline (AC-001-3).
- Single-flight: um contexto llama por processo; requisições concorrentes serializam (AC-001-4).
- Modo dormir com teto rígido de tokens (BR-006) e nunca concorrente com turns (BR-001).
- Privacidade: dados só saem da máquina se o usuário configurar um modelo externo
  (chaves) — o fluxo nativo é 100% local.

## Avaliação

Baseline (tok/s prefill/geração, RSS, taxa de tool-call válido) será medida com
`cmd/nativebench` na máquina alvo no E7 e registrada como S39 — sem números inventados
antes da medição (coverage S39 = tbd).
