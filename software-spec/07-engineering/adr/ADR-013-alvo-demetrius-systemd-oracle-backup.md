---
id: ADR-013
title: Alvo de produção passa a ser a VM Google `demetrius` (systemd, sem Docker em runtime); Oracle vira backup
status: accepted
version: 1
owner: André
last_updated: 2026-09-12
depends_on: [ADR-001, ADR-009, ADR-012]
---

# ADR-013 — Alvo de produção: `demetrius` sob systemd; Oracle como backup

## Contexto
O E7 (ADR-011/012, S18 v3, S29 v3, S32 v3) deixou a Oracle `VM.Standard.E2.1.Micro`
tecnicamente pronta (imagem `kuromatsu:native-amd64` funcional, AVX2 pinado). Mas a
shape é **inutilizável na prática**: mede-se apenas **~25% de CPU real (`steal` 75%)**
— nenhuma mudança de código resolve um teto impposto pela plataforma.

Levantamento (só leitura, 2026-09-11) da VM Google `demetrius` (`e2-micro`, dedicada ao
Kuromatsu, sem custo extra — Always Free do GCP):

| Item | Valor |
|---|---|
| CPU | `Intel Xeon @ 2.20GHz`, 1 core físico/2 threads (HT), AVX2+FMA+F16C, **sem AVX-512** — o host físico do e2-micro **muda a cada boot** (teste anterior do André mediu um `EPYC 7B12`) |
| RAM | 969 MB total, 466 MB disponíveis em idle (antes do trim) |
| Swap | `zram0` 993 MB zstd ativo; `swappiness=10` (baixo — prioriza descartar page cache do modelo a comprimir heap) |
| SO | Debian 12, kernel 6.1, systemd 252, cgroup v2, `sudo` sem senha |
| Docker | 29.8 instalado, sem containers/imagens |
| Modelo | GGUF já presente em `~/stacks-agent/llama.cpp/build/prism-ml/`, mesmo SHA256 do nosso |
| Rede | Telegram API alcançável por long-polling (sem porta de entrada) |
| Vazão medida | **5,1 tok/s de geração e 7,3 tok/s de prompt, `steal` 0** — bate a NFR-002 (≥4 tok/s) |

## Decisão
1. **Alvo de produção = `demetrius`** (Google Cloud `e2-micro`). O binário
   `kuromatsu` roda **direto sob systemd** (`Type=simple` inicialmente; `Type=notify` +
   `NotifyAccess=main` + `WatchdogSec` quando ADR-016 entregar `sd_notify`) — **sem
   Docker em runtime**. Docker fica instalado (já estava) porém **desativado**
   (`docker.socket docker containerd`), liberando a RAM que `dockerd+containerd`
   ocupavam (~51 MB).
2. A imagem `kuromatsu:native-amd64` (ADR-011/012) **continua sendo o artefato de
   build do CI** — ambiente de build reprodutível, não vira o veículo de deploy. O CI
   (`build-native-image.yml`) passa a publicar **também** os binários crus
   (`kuromatsu-native-linux-amd64`, `nativebench-linux-amd64`) extraídos da imagem, que
   é o que de fato viaja para a `demetrius` (`scp` + `systemctl restart`, via
   `scripts/deploy-demetrius.sh`).
3. **Oracle `VM.Standard.E2.1.Micro` vira alvo de backup**, não roda inferência (o
   `steal` 75% a torna inviável para isso): recebe `rsync` cross-cloud (push, sem custo)
   do backup noturno gerado na `demetrius` (ver S33).
4. **Pin AVX2 do ADR-012 é mantido, não relaxado** — como o host físico do e2-micro
   varia por boot (Xeon observado nesta sessão, EPYC 7B12 numa sessão anterior do
   André), a build precisa continuar cobrindo ambos: `GGML_AVX2=ON` +
   `GGML_AVX512*=OFF` funciona nos dois, nenhum dos dois expõe AVX-512-VNNI (logo o
   kernel GEMM repack otimizado do `Q1_0` nunca está disponível de qualquer forma —
   ver S18).
5. **CPU tratada como orçamento, não como recurso ilimitado** — o e2-micro usa um
   modelo de *bursting* (0,25 vCPU sustentado + créditos de burst); ver R7/R8 em S06 e
   a mitigação via ADR-015 (cache de prefixo torna o custo por turno o delta, não o
   prompt completo, mesmo no piso estrangulado).

## Alternativas
- **Manter a Oracle como produção e só otimizar software**: rejeitada — `steal` 75% é
  um teto da plataforma (VM compartilhada/CPU-starved), não algo que otimização de
  prompt ou cache resolvam; a `demetrius` mede `steal` 0 rodando o mesmo binário.
- **Rodar a `demetrius` via Docker também (paridade com a imagem de CI)**: rejeitada
  para runtime — em 1 GB de RAM, o trim do SO (Trilho 0) recupera ~200 MB largando
  exatamente os serviços que o Docker precisa (`dockerd`, `containerd`); manter o
  container em produção devolveria parte dessa RAM recuperada. A imagem continua
  existindo como artefato de CI/ambiente de build determinístico.
- **G0 como gate go/no-go** (teste de 5 min): corrigido nesta sessão (comentário do
  André) — vira uma *medição* (burst × piso estrangulado, ~20 min com `vmstat 5`); a
  migração não depende do piso porque a ADR-015 torna o custo por turno o delta.

## Consequências
- `docker/data/config.json` do backup aponta para `llama-server:8080` — **não pode ser
  copiado como está** para a `demetrius` (esse endpoint não existe mais desde o
  provider nativo); o `config.json` novo precisa declarar
  `agents.defaults.model_name: bonsai-local` explicitamente (Trilho 0, passo 4).
- Operar dois provedores de nuvem (GCP produção + Oracle backup) em vez de um só —
  aceito; o custo é zero em ambos (Always Free), e a redundância cross-cloud é
  positiva para S33 (disaster recovery).
- `deploy/systemd/kuromatsu.service` (+ `kuromatsu-backup.timer`) substitui
  `docker-compose.yml` como interface operacional na `demetrius` — runbook novo
  (`systemctl status/restart/journalctl`), sem os comandos `docker compose` de antes.
- `.github/workflows/build-native-image.yml` ganha um passo de publicação de binário
  cru, além do artefato de imagem já existente.
