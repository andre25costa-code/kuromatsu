---
id: S39
title: Baseline de desempenho — benchmark quantitativo na demetrius
status: confirmed
version: 1
owner: André
last_updated: 2026-09-13
depends_on: [S06, S18, S21, S29, S34]
---

# S39 — Baseline de desempenho (demetrius, 2026-09-13)

> Primeira rodada de medição **real** (não estimada) dos Trilhos A/B/C
> (ADR-014..018) em produção, na VM `demetrius` (Google `e2-micro`), logo
> após o deploy dos binários novos. Metodologia: valores medidos via log
> real (`journalctl`), `ps`/`vmstat`/`free`, `nativebench` e o próprio
> `kuromatsu agent`/gateway — nenhum número abaixo foi estimado ou
> extrapolado sem dizer isso explicitamente. Onde a condição exigida pela
> NFR não foi reproduzida (ex.: "burst pleno"), o campo diz isso em vez de
> inventar um valor, seguindo a mesma regra já aplicada em S29/S40/BACKLOG.

**Contexto que afeta a leitura de todos os números abaixo**: a sessão de
medição começou imediatamente após ~40 minutos de atividade pesada de CPU
na `demetrius` (build/deploy/CI, ver `entregues`/`BACKLOG.md`), então os
créditos de burst do e2-micro já estavam parcialmente ou totalmente
exauridos desde o primeiro teste. Isso é o mesmo viés já registrado no G0
(`.claude/team/research/g0-medicao-real.md`) — mas nesta rodada, ao
contrário do G0, temos **múltiplas medições independentes espalhadas por
~2h de relógio**, o que permite ver o *padrão de recuperação* dos créditos
ao longo do tempo (ver M2), não só um único vale.

## Sumário executivo

| Critério | Resultado |
|---|---|
| Cache de prefixo do KV (Trilho B1) | **Confirmado, dentro do turno e entre turnos** — 98,3% e 88,0% de reaproveitamento reais, ~40× e ~11× de aceleração do prefill |
| Motor de estados / rejeição de tool alucinada (Trilho A4) | **Confirmado** — nome de tool vazio rejeitado sem escalar janela |
| Regressão `PICOCLAW_*` → `KUROMATSU_*` (ADR-005) | **Confirmado em produção** (não só no teste unitário) |
| `NThreads = min(NumCPU,4)` (ADR-015) | **Confirmado como necessário** — `n_threads=4` nesta VM (2 CPUs lógicas) trava com zero conclusões em 14+ min |
| Memória (NFR-001/008) | **Dentro da meta**, com folga |
| Cancelamento gracioso no shutdown | **Gap real encontrado** — ver "Achado crítico" abaixo; não corrigido nesta sessão, decisão do André |
| Overhead de framework por janela (NFR-007) | **Possível gap** — total medido da janela `heartbeat` (2603 tokens) é bem maior que o esperado; recomenda-se medir isoladamente (A9) |

## Achado crítico: shutdown gracioso não cancela inferência em andamento

Ao parar o serviço (`systemctl stop kuromatsu`) com um turno em andamento
(iteração 2 do heartbeat, em decode):

```
21:18:30  systemd: Stopping kuromatsu.service...
21:18:30  gateway:  Shutting down...
21:18:31  channels/telegram/heartbeat/dispatchers: todos pararam (< 1 s)
21:20:00  systemd: State 'stop-sigterm' timed out. Killing.
21:20:00  systemd: Killing process 81612 (kuromatsu) with signal SIGKILL.
21:20:01  systemd: Main process exited, code=killed, status=9/KILL
```

O desligamento dos canais/heartbeat/dispatchers foi limpo e rápido (<1s),
mas a chamada de inferência já em voo **não foi cancelada** — o processo
ficou vivo até o timeout padrão do systemd (`stop-sigterm`, 90s) e foi
morto à força. O `abort_callback` do Trilho B0 (ADR-015) existe e funciona
para cancelamento de turno via `ctx.Done()` (timeout/usuário), mas o fluxo
de shutdown do gateway (`pkg/gateway/gateway.go`) não parece propagar
cancelamento para o turno em execução no momento do `Shutting down...`.

Consequência operacional adicional, também descoberta ao vivo: depois do
`SIGKILL`, o `systemctl start kuromatsu` **não reiniciou** o serviço — ele
ficou em `failed (Result: timeout)` e precisou de `systemctl reset-failed`
explícito antes do `start` funcionar.

**Isto não foi corrigido nesta sessão.** Fica registrado como decisão
aberta para o André: (a) abrir ADR/backlog para propagar o cancelamento do
contexto de shutdown até o turno em voo, e/ou (b) reduzir
`TimeoutStopSec` no unit systemd para falhar mais rápido, e/ou (c)
documentar como comportamento aceito (a VM é de uso pessoal, reinícios
raros). Ver `software-spec/BACKLOG.md`.

## M1 — Vazão do engine (NFR-002: geração ≥ 4 tok/s)

NFR-002 é medida em condição de **burst**; o piso sob créditos exauridos é
informativo (ver nota em S29). Nesta sessão só foi possível medir o piso —
a VM já estava sob throttle desde o primeiro teste.

| Condição | pp (tok/s) | tg (tok/s) | Fonte | Quando (UTC) |
|---|---|---|---|---|
| Burst (créditos disponíveis) | 7,3 | 5,1 | Medição do André, anterior a esta sessão — **não remedida nesta rodada** | — |
| Piso, `nativebench` 1ª chamada após início do teste | 11,9¹ | 8,96 | `n_threads=1`, prompt de 39 tokens | 21:21:07 |
| Piso, `nativebench` chamadas seguintes (mesma sessão, ~1 min depois) | 1,4–11,8 | 1,07–2,13 | `n_threads=1`, decaindo | 21:21–21:22 |
| Piso, heartbeat real (turno completo, logo após deploy+CI) | 0,97 (cold) / 0,67 (delta) | 0,44–0,48 | `main-turn-1`/`main-turn-7`, prompt real ~2,6k tokens | 20:59–21:15 |
| Piso, `nativebench` estabilizado (30+ min depois, warm-up repeat×3) | 1,4–1,7 | 1,17–1,27 | `n_threads=2`, prompts curtos (38-42 tok) | 21:38–21:46 |

¹ Esta primeira leitura de 11,9/8,96 tok/s é um resquício de créditos de
burst ainda não totalmente exauridos no instante exato em que o teste
começou — cai para a faixa do piso já na 3ª chamada, 75s depois.

**Leitura**: diferente do G0 (um único vale), esta rodada mostra uma
**recuperação parcial** ao longo de ~30-40 minutos, do piso mais severo
(~0,44-0,48 tok/s, medido nos turnos de heartbeat logo após o
build/deploy) para um piso mais estável (~1,2-1,7 tok/s, medido no
`nativebench` uma hora depois). Isso é consistente com a hipótese do G0
("o vale medido pode ser pior que o platô real") — os créditos de burst
parecem se recompor de forma gradual, não binária. **Ainda não medido**:
o piso "burst pleno" numa VM fria (sem nenhuma atividade prévia na
sessão) — mantém-se a recomendação do G0 de remedir isso separadamente
antes de fechar um número único de baseline.

**Score M1 (NFR-002, gate de burst)**: não regride — burst não foi
remedido, mas nada nesta sessão contradiz o número anterior (7,3/5,1).
Gate de burst permanece **PASS** por medição anterior, não desta sessão.

## M2 — Efeito de threads e oversubscription

| `n_threads` | Resultado | Interpretação |
|---|---|---|
| 1 | 3 chamadas completas, tg 8,96→2,13→1,07 tok/s | Decaimento de crédito de burst dentro de ~75s |
| 2 | 3 chamadas completas, tg 1,18→1,17→1,22 tok/s | Já no piso (rodou depois de threads=1 ter consumido crédito — **comparação com threads=1 é confundida pelo tempo/crédito, não isola o efeito de threads puro**) |
| 4 | **Abortado após 14 min, ZERO conclusões** (`nproc`=2 nesta VM) | Oversubscription severo faz o processo nunca terminar um único prompt de 48 tokens |

**Score M2**: `n_threads=4` trava a VM. Isto **valida diretamente** a
correção do ADR-015 (`NThreads = min(NumCPU, 4)` em vez de fixo em 4) —
nesta VM (`NumCPU=2`), o valor de produção é 2, evitando exatamente este
colapso. **PASS** para a decisão de design; **achado** de que comparar
1 vs. 2 threads de forma isolada exigiria repetir os testes em ordem
aleatória ou em VMs separadas (não feito aqui por tempo).

## M3 — Cache de prefixo do KV, dentro do mesmo turno (NFR-009, meta ≥70%)

Turno `main-turn-1` (heartbeat, janela `heartbeat`, 3 tools):

| Iteração | prompt_tokens | cached_tokens | hit % | prefill_ms | gen_ms | output_tokens |
|---|---|---|---|---|---|---|
| 1 (fria) | 2603 | 0 | 0% | 2 684 133 (44,7 min) | 195 866 (3,3 min) | 86 |
| 2 (após tool call rejeitado) | 2644 | **2599** | **98,3%** | 66 983 (67 s) | 94 768 (95 s) | 45 |

Aceleração do prefill entre iterações do mesmo turno: **~40×** (2684,1s →
67,0s), atribuível inteiramente ao cache de prefixo — o delta real
reprocessado caiu de 2603 para 45 tokens.

**Score M3**: 98,3% ≫ meta de 70% (NFR-009). **PASS, com margem grande.**

## M4 — Cache de prefixo do KV, entre turnos de heartbeat (achado novo, não previsto em nenhuma NFR existente)

Turno `main-turn-7`, iniciado 4 min depois do fim de `main-turn-1`, com
contexto de turno **completamente novo** (2 mensagens, sem histórico —
por desenho, heartbeat usa `NoHistory`):

| Iteração | prompt_tokens | cached_tokens | hit % | prefill_ms | gen_ms |
|---|---|---|---|---|---|
| 1 | 2603 | **2290** | **88,0%** | 240 759 (4,0 min) | 274 496 (4,6 min) |

Isto prova que o cache de prefixo do Trilho B1 **persiste entre execuções
separadas do heartbeat** (não é limpo entre turnos), porque o prompt do
heartbeat é quase idêntico a cada disparo (mesmo template, mesmo tamanho
de mensagem — `user_len=1112` em ambos os turnos). Aceleração de prefill
vs. o turno frio original: **~11×** (2684,1s → 240,8s).

**Score M4**: não existe meta formal para isto ainda (é reaproveitamento
*entre* turnos, não *dentro* do turno, que é o que NFR-009 mede
literalmente) — mas é o resultado prático mais valioso para o caso de uso
real (heartbeat roda a cada hora, sempre a mesma janela). Recomenda-se
adicionar isto como um AC explícito do Trilho B (ex.: "cache de prefixo
entre disparos consecutivos do heartbeat ≥ 80%").

## M5 — Rejeição de tool alucinada / escalonamento (Trilho A4, ADR-014)

Durante `main-turn-7`, o modelo pediu 2 tool calls na mesma resposta: uma
com nome vazio (`""`) e uma `sysmon`. Log real:

```
21:15:19 agent.llm.response tool_calls=2 tools=["","sysmon"]
21:15:19 agent.tool.exec_skipped reason="ferramenta \"\" não existe;
         disponíveis nesta janela: cron, message, sysmon"
21:15:19 tool sysmon: "property \"pid\": expected integer, got string"
```

A tool de nome vazio foi rejeitada **imediatamente, sem escalar a janela**
e com uma dica das tools disponíveis — exatamente o desenho do passo 1 do
A4 (verificação no registro global antes de escalar). A chamada `sysmon`
foi rejeitada por validação de schema (tipo errado), também sem custo de
KV. Nenhuma das duas rejeições custou prefill adicional.

**Score M5**: **PASS** — comportamento correto observado organicamente em
produção (não foi um teste sintético isolado, mas ainda mais valioso por
isso).

## M6 — Regressão `PICOCLAW_*` → `KUROMATSU_*` (ADR-005)

Além do teste unitário corrigido em `pkg/config/env_compat_test.go`
(`go test ./pkg/config/... ` → PASS), a regressão foi confirmada **no
binário real implantado**:

```
21:49:27 WRN config env_compat.go:45 > PICOCLAW_TEST_BENCH_VALUE is
         deprecated, use KUROMATSU_TEST_BENCH_VALUE instead
         new_env=KUROMATSU_TEST_BENCH_VALUE old_env=PICOCLAW_TEST_BENCH_VALUE
```

**Score M6**: **PASS**, confirmado em dois níveis (unitário + produção).

## M7 — Memória (NFR-001, NFR-008)

| Momento | RSS processo (`ps`) | cgroup `Memory` (systemd) | `MemAvailable` sistema |
|---|---|---|---|
| Idle, gateway recém-iniciado, modelo não carregado | — | 87,5 MB | 762,4 MB |
| ~30s depois, modelo carregado + 1ª inferência em andamento | 590,4 MB | 375,1 MB¹ | 390 MB |
| Durante turno de heartbeat real (main-turn-1/7, picos observados) | 400-590 MB | — | 338-467 MB livre (`vmstat`) |

¹ A diferença entre RSS do processo (590 MB) e `Memory` do cgroup
(375 MB) é esperada — o `systemd`/cgroup v2 conta memória residente de
forma diferente do `ps` (paginas compartilhadas/mmap do modelo GGUF
contam de forma distinta). Ambos os números ficam bem abaixo do limite de
900 MB (`MemoryMax`) e da meta de 850 MB (NFR-001).

**Score NFR-001** (RSS < 850 MB): **PASS**, com folga (~260-500 MB abaixo
do limite em todos os pontos medidos).

**Score NFR-008** (`MemAvailable` ≥ 250 MB com modelo carregado): **PASS**
— 390 MB e 338-467 MB medidos com modelo ativo, acima da meta.

## M8 — Overhead de framework por janela (NFR-007, meta: heartbeat ≤300 tokens, chat ≤450, total ≤800)

O `prompt_tokens` real da janela `heartbeat` (mensagens=2: system+user,
tools=3) medido em produção foi **2603 tokens** — muito acima da meta de
≤300 tokens de overhead de framework para esta janela, mesmo descontando
os ~1,0-1,2k tokens de conteúdo de workspace do usuário (AGENT/SOUL/
USER/MEMORY, que por desenho são custo aditivo do usuário, não do
framework — ver ADR-014/A9). Sobra uma diferença não explicada de
aproximadamente 1,4k tokens.

**Isto não foi isolado nesta sessão** (exigiria rodar o teste
`prompt_size_test`/A9 com um workspace mínimo controlado, que é um teste
Go, não uma medição de campo) — então não é possível afirmar com certeza
se é: (a) o modo `compact` do system prompt não está tão compacto quanto
o design pretendia, (b) o conteúdo real de workspace do André é maior que
a estimativa de ~1,0-1,2k, ou (c) alguma combinação. **Recomenda-se
fortemente rodar `go test ./pkg/agent/... -run PromptSize` (ou equivalente
A9) e comparar com este número de campo antes de fechar NFR-007.**

**Score M8 (NFR-007)**: **flag amarelo / não confirmado** — o número
medido em campo é consistente com um possível gap, mas a causa não foi
isolada. Não marcar como FAIL sem o teste isolado (regra de não
invenção).

## M9 — Disponibilidade e observabilidade do runstate (NFR-005, C1-C3)

- Nenhuma falha de tool (validação de schema, política de canal rejeitada)
  derrubou o processo ou interrompeu o turno — o agente sempre seguiu
  para a iteração seguinte ou concluiu normalmente. **PASS (NFR-005,
  evidência indireta mas real, observada em 2 turnos distintos).**
- `run/state` e `/health` refletiram o estado real (`bits=1
  names=inference`) durante inferência ativa, confirmando a superfície de
  observabilidade do Trilho C (C1/C2). **PASS.**
- `heartbeat: Skipping heartbeat: runstate busy (AC-017-2)` foi logado
  corretamente a cada tick enquanto o motor estava ocupado, nunca
  enfileirando um heartbeat atrasado. **PASS (AC-017-2 confirmado ao
  vivo).**
- `memguard`: `moderate memory pressure: ran GC/FreeOSMemory` disparou
  corretamente sob pressão real (múltiplas ocorrências durante os testes),
  sem nenhum OOM-kill do kernel observado (`dmesg` não verificado
  diretamente nesta sessão — **não confirmado por esta via específica**,
  mas a ausência de qualquer reinício inesperado do processo por OOM é
  evidência indireta). **PASS, com uma ressalva de verificação.**

## Achados de CI/build (não são medição de runtime, mas são reais e quantitativos)

Dois bugs reais foram encontrados e corrigidos **apenas porque o pipeline
de CI foi de fato executado** (não seriam detectáveis por revisão de
código):

1. `undefined reference to 'kuromatsu_abort_cb'` — função `static` do C
   não pode ter o endereço tomado via `C.funcname` pelo código gerado do
   cgo. Corrigido removendo `static` (linkage externa).
2. `pull access denied ... repository does not exist` — o passo de
   extração de binários crus tentava `docker create` sem antes `docker
   load` do tarball exportado por `outputs: type=docker,dest=<tar>` (que
   não carrega no daemon local). Corrigido com `docker load -i` antes da
   extração.

## O que NÃO foi medido nesta sessão (não inventar)

- Piso "burst pleno" numa VM fria (sem atividade prévia na sessão) —
  recomendação do G0 permanece válida.
- NFR-003 (portabilidade cross-GOOS/GOARCH) e NFR-004 (liberação de RSS
  após `keep_alive_secs` de inatividade) — nenhum dos dois foi exercitado
  nesta rodada.
- NFR-006 (turno típico ≤90s) **em condição de burst** — todos os turnos
  medidos ocorreram sob o piso estrangulado; sob burst, extrapolando pelos
  fatores de aceleração medidos entre burst e piso (~4-12×), a iteração 2
  cacheada de `main-turn-1` (161,75s no piso) projetaria para
  ~15-40s em burst — dentro da meta, mas isto é uma **projeção, não uma
  medição direta**.
- Suítes 2/3 completas do `scripts/benchmark-demetrius.sh` (chamadas frias
  por CLI para as janelas `files`/`shell`/`memory`/`full`, e o teste de
  escalonamento sintético) — não executadas por restrição de tempo de
  sessão (cada chamada fria sob o piso estava levando 25-50+ minutos); o
  Trilho A4 já foi validado organicamente em produção (M5), o que reduz o
  valor marginal de repetir um teste sintético equivalente.
- `dmesg` para confirmar ausência de OOM-kill diretamente (só evidência
  indireta de que o processo não reiniciou inesperadamente).

## Metodologia / reprodutibilidade

- Script: `scripts/benchmark-demetrius.sh` (suítes 1, 4, 6, 7 executadas;
  suítes 2/3/5 parcialmente substituídas por observação direta do
  gateway ao vivo, que rendeu dados mais ricos que o script sintético
  teria produzido).
- Binários: `kuromatsu`/`nativebench` buildados no CI (3ª tentativa, após
  os 2 bugs acima), sha256 confirmados como ELF x86-64 reais antes do
  deploy.
- Toda a medição foi feita com o gateway em produção real na `demetrius`
  (não em ambiente sintético/local), com tráfego real do Telegram
  intercalado (comandos `/start`, `/show`, `/help`, `/stats` do usuário
  durante os testes, todos respondidos corretamente e sem interferir nos
  turnos medidos — outra evidência indireta de NFR-005).
