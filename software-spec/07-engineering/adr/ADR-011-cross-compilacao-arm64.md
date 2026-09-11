---
id: ADR-011
title: Build da imagem nativa via cross-toolchain aarch64, não QEMU
status: accepted
version: 1
owner: André
last_updated: 2026-09-11
depends_on: [ADR-001, ADR-009]
---

# ADR-011 — Cross-compilação real para arm64 em vez de emulação QEMU

## Contexto
O E5 mediu, na prática, que compilar `ggml`/`llama.cpp` (C++, estático) e linkar o
binário cgo completo **estoura a memória do host de desenvolvimento mesmo compilando
nativamente para x86_64** — 4 travas por falta de memória e uma queda completa da VM
WSL, só contornadas reduzindo o paralelismo do cmake até `-j 1`. O plano original do
E5/E7 (ver `llama-lib-arm64`/`build-native-arm64` no Makefile e o pipeline do S32 v1)
assumia rodar esse mesmo build **dentro** de um ambiente linux/arm64 — nativo ou, no
caso mais provável (host de dev é x86_64), emulado via QEMU sob `docker buildx
--platform linux/arm64`. Emulação de instruções é tipicamente 5-20× mais lenta e
consome mais memória por processo do que execução nativa — exatamente o tipo de custo
que já derrubou o host compilando nativamente. Rodar a mesma carga de trabalho pesada
sob QEMU teria alta chance de repetir ou agravar o problema, e não há tolerância de
memória sobrando para testar isso por tentativa e erro.

A intenção do André, confirmada nesta conversa, é: nunca compilar na máquina Oracle de
1 GB (não tem toolchain nem RAM para isso) — o build acontece em outra máquina, e só a
**imagem Docker pronta** é levada para o servidor. Medido nesta mesma conversa: o host
de dev tem só ~8 GB de RAM física total (com bem menos que isso livre na prática), teto
que já causou o crash da VM WSL no E5 — não é folga suficiente para tentar de novo com
segurança, mesmo com o cross-toolchain. Decisão: usar o runner **arm64 nativo hospedado
pelo GitHub** (`ubuntu-24.04-arm`, 4 vCPU/16 GB) para este build específico, em vez do
host de dev.

## Decisão
1. O estágio pesado (cmake + g++ da `ggml`/`llama.cpp`, e o `go build -tags nativellm`)
   roda **nativamente**, nunca sob QEMU — `docker/Dockerfile.native` detecta em build
   time (`uname -m`) se o host do estágio builder já é arm64 ou é amd64:
   - **arm64 nativo** (`.github/workflows/build-native-image.yml`, runner
     `ubuntu-24.04-arm`): compila direto com `gcc`/`g++` do sistema. **Este é o
     caminho principal** — evita por completo o teto de RAM do host de dev.
   - **amd64** (uso local opcional, numa máquina com mais RAM que o host de dev
     atual): cross-compila com `aarch64-linux-gnu-gcc`/`g++`
     (`crossbuild-essential-arm64` no Debian) — `CMAKE_SYSTEM_NAME=Linux` +
     `CMAKE_SYSTEM_PROCESSOR=aarch64` + `CMAKE_C/CXX_COMPILER` no cmake, e
     `CC=aarch64-linux-gnu-gcc CXX=aarch64-linux-gnu-g++ CGO_ENABLED=1 GOOS=linux
     GOARCH=arm64` no `go build`. Isso substitui a suposição anterior de "cgo não
     cross-compila o lado C" — cgo cross-compila normalmente quando `CC`/`CXX`
     apontam para um cross-compiler válido.
   Em ambos os casos, `GGML_CPU_ARM_ARCH=armv8.2-a+dotprod+fp16` (sem i8mm —
   Ampere/Neoverse-N1) é fixado explicitamente, então o binário resultante sempre
   mira o mesmo alvo (Oracle A1) independente de qual CPU realmente compilou.
2. `docker/Dockerfile.native` isola a emulação (quando o build roda em amd64) ao
   mínimo possível: o estágio builder roda com `--platform=$BUILDPLATFORM` (nativo,
   nunca emulado) e faz todo o trabalho pesado; só o estágio final
   (`FROM --platform=linux/arm64 debian:bookworm-slim` + `apt-get install` de
   pacotes leves — sem compilar nada) roda sob emulação quando o host não é arm64,
   e isso é E/S-bound (baixar/descompactar `.deb`), não CPU/memória-bound como
   compilar C++. No runner nativo arm64 nem essa emulação acontece.
3. Entrega da imagem ao servidor: **sem registry** por padrão. O workflow do GitHub
   Actions exporta a imagem como artefato baixável (`.tar.gz`, via
   `docker/build-push-action` com `outputs: type=docker`); localmente o equivalente é
   `docker save kuromatsu:native-arm64 | gzip > kuromatsu-native-arm64.tar.gz`
   (`make docker-save-native`). De qualquer uma das duas origens: baixar/copiar o
   `.tar.gz`, `scp` para a Oracle, `docker load` lá. Publicar num registry
   (GHCR/Docker Hub) fica como opção futura, não necessária para o fluxo manual atual.

## Alternativas
- **Build no host de dev via QEMU completo** (`docker buildx --platform linux/arm64`
  sem cross-toolchain, como o Makefile assumia até aqui): mais simples de escrever,
  mas reintroduz o mesmo risco de OOM/crash já sofrido no E5, agora sob emulação
  (pior), e ainda estaria limitado pelos ~8 GB do host de dev. Rejeitada.
- **Build no host de dev via cross-toolchain** (sem emulação, mas ainda no host de
  8 GB): resolve o custo extra do QEMU, mas não resolve o teto de RAM real da
  máquina — o link final do binário Go+cgo já havia falhado mesmo sem emulação.
  Mantida como opção documentada (`make docker-build-native`/`docker-save-native`)
  para quando o host de dev tiver mais RAM livre, mas não é o caminho padrão.
- **Compilar direto na Oracle**: a VM alvo tem 1 GB de RAM e nenhum toolchain C++
  instalado — nem cogitado (viola a premissa do C1/S06). Rejeitada.
- **Provisionar uma VM arm64 maior só para build** (ex. outra instância Oracle Ampere
  com mais RAM): funciona, mas exige manter um segundo host próprio. O runner do
  GitHub Actions cobre a mesma necessidade sem infraestrutura extra — **escolhida**.

## Consequências
- `Makefile`: `llama-lib-arm64`/`build-native-arm64` recebem `ARM64_CC`/`ARM64_CXX`
  configuráveis (default = cross-toolchain `aarch64-linux-gnu-*`); o
  `docker/Dockerfile.native` decide em build time qual usar via `uname -m`.
- Diretório de build do cmake para arm64 é separado do host
  (`llama.cpp/build-native-arm64/`, espelhado para `llama.cpp/build-native/lib` só
  para casar com o caminho fixo do `#cgo LDFLAGS`) — evita colisão de cache do CMake
  com um `make llama-lib` (host) já rodado na mesma árvore.
- Novo `.github/workflows/build-native-image.yml` (`workflow_dispatch`, runner
  `ubuntu-24.04-arm`): caminho principal para gerar a imagem; produz um artefato
  `.tar.gz` baixável, sem precisar de registry nem do host de dev.
- Requisito no host de build **local** (opcional/fallback): pacote
  `crossbuild-essential-arm64` (ou equivalente) instalado — documentar na skill
  `kuromatsu-docs` (E8) junto com o fluxo do GitHub Actions (caminho principal).
- Se o cross-toolchain não conseguir compilar algum kernel NEON específico do fork
  prism (risco baixo, mas não zero) no caminho local opcional, isso não afeta o
  runner nativo arm64 do GitHub (compila com `gcc`/`g++` normal, sem cross).
