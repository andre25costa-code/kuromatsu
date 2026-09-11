---
id: ADR-012
title: Alvo de deploy é x86_64 (EPYC 7551/Zen1), não ARM64 — corrige ADR-011
status: accepted
version: 1
owner: André
last_updated: 2026-09-11
depends_on: [ADR-001, ADR-009, ADR-011]
---

# ADR-012 — Correção: a VM Oracle é x86_64, não ARM64

## Contexto
Todo o design do E7 (ADR-011, S18 v2, S29, S32 v1/v2) assumiu a VM Oracle como
**ARM64 (Ampere A1/Neoverse N1)** — premissa A1 do S06, nunca verificada com `lscpu`
antes de construir em cima dela. O André reportou, depois do primeiro build
bem-sucedido no runner arm64 do GitHub, que **não conseguiu disponibilidade da shape
A1** no momento da criação da VM e por isso criou a instância com a shape x86 padrão
da Oracle — o que também explica por que a VM só tem 1 GB de RAM (a A1 chegaria a
6 GB nessa configuração). A shape x86 "Always Free" da Oracle é a
`VM.Standard.E2.1.Micro`, sobre **AMD EPYC 7551 "Naples" (Zen1, lançado em 2017)** —
ainda não confirmado com `lscpu` na própria VM, mas é a única shape x86 gratuita da
Oracle, então a inferência é de alta confiança.

Zen1 tem AVX2, FMA3, F16C, BMI1/2, MOVBE (perfil equivalente a `x86-64-v3`) — **não**
tem AVX-512 nem AVX-VNNI (isso só chegou em CPUs AMD com o Zen4/EPYC Genoa, 2022).
Inspecionando o código do fork prism: `ggml_vec_dot_q1_0_q8_0`
(`ggml/src/ggml-cpu/arch/x86/quants.c:645`) tem um caminho AVX2 dedicado — bom, isso
cobre a Oracle. Já a GEMM repack otimizada de `q1_0`
(`ggml_gemm_q1_0_4x8_q8_0`/`ggml_gemv_q1_0_4x8_q8_0`, `arch/x86/repack.cpp:6409`) é
guardada por `#if defined(__AVX512F__) && __AVX512BW__ && __AVX512DQ__ &&
__AVX512VNNI__` — **indisponível no Zen1**, então na Oracle real esse caminho cai para
o AVX2 de dot-product (mesma classe de perda de desempenho que a hipótese ARM64 já
previa sem i8mm — não é uma regressão nova, é o mesmo tipo de trade-off, só que no
eixo x86).

## Decisão
1. O alvo de build/deploy passa a ser **x86_64/amd64**, com CPU pinada em
   `GGML_NATIVE=OFF` + `GGML_AVX=ON GGML_AVX2=ON GGML_FMA=ON GGML_F16C=ON` e
   `GGML_AVX512*=OFF GGML_AVX_VNNI=OFF` explícitos — equivalente x86 do que
   `armv8.2-a+dotprod+fp16` era para ARM: evita que o runner de build (que pode ter
   uma CPU mais nova, com AVX-512) gere um binário que dá SIGILL na Oracle.
2. **Isso elimina a necessidade de cross-compilação inteiramente**: build host
   (GitHub Actions), máquina de dev (Windows/WSL x86_64) e target de deploy (Oracle
   x86_64) são todos a mesma arquitetura. O raciocínio da ADR-011 sobre evitar
   emulação QEMU continua correto em espírito, mas agora é automático — não existe
   mais um cenário de plataforma cruzada para evitar.
3. `.github/workflows/build-native-image.yml` muda o runner de `ubuntu-24.04-arm`
   para **`ubuntu-latest`** (amd64 padrão, mesma config de 4 vCPU/16 GB) —
   continua sendo o caminho principal, pelos mesmos motivos da ADR-011 (o host de dev
   tem só ~8 GB físicos e já travou compilando isso).
4. `docker/Dockerfile.native` perde toda a lógica de detecção `uname -m`/toolchain
   condicional da ADR-011 (não faz mais sentido com uma única arquitetura em jogo) —
   volta a ser um Dockerfile multi-stage simples, sem flags de `--platform` especiais.
5. Tags/nomes atualizados de `*-arm64` para `*-amd64` (imagem `kuromatsu:native-amd64`,
   artefato `kuromatsu-native-amd64.tar.gz`) para não sugerir uma arquitetura errada.
6. O suporte ARM64 já construído (`llama-lib-arm64`/`build-native-arm64`,
   `ubuntu-24.04-arm`) **não é removido** — vira um caminho secundário/futuro
   (ex. Raspberry Pi, ou se a shape A1 ficar disponível depois), documentado como tal.

## Alternativas
- **Descartar todo o trabalho ARM64 do E7**: desperdiça um caminho já validado de
  ponta a ponta e que pode servir outros alvos (Raspberry Pi arm64 já é um target do
  `make build-linux-arm64` na build matrix comum). Rejeitada — manter como opção
  secundária custa só a manutenção de mais um par de targets no Makefile.
- **Não pinar CPU no x86, usar `GGML_NATIVE=ON`**: mais simples, mas reabre o mesmo
  risco de SIGILL que a pinagem ARM já existia para evitar, já que o runner de CI e a
  Oracle não são a mesma CPU física. Rejeitada.

## Consequências
- `software-spec/01-business/06-assumptions.md`: A1 corrigida, C7 e R3 atualizados.
- S18, S29, S32 atualizados (Kernels x86_64/AVX2, CPU do NFR-002, pipeline sem
  cross-compile).
- `Makefile`: novos `llama-lib-x86-64`/`build-native-x86-64` (pinados,
  AVX2+FMA+F16C, sem cross-toolchain) tornam-se o caminho usado por
  `docker/Dockerfile.native`; `llama-lib-arm64`/`build-native-arm64` continuam
  existindo, agora documentados como secundários.
- `.github/workflows/build-native-image.yml`: runner trocado, plataforma trocada,
  nomes de imagem/artefato trocados.
- Pendência: confirmar com `lscpu` na própria VM Oracle, no primeiro acesso, que é
  de fato EPYC 7551/Zen1 (ou ao menos AVX2-capable sem AVX-512) — se for uma CPU
  diferente do esperado, os flags de `GGML_AVX*` podem precisar de ajuste.
