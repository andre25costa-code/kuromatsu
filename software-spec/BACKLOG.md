# BACKLOG — o que falta

> **Reconciliação 2026-09-16** — As seções históricas abaixo não representam
> integralmente o estado atual. Integração do sono, validação, publicação dos
> binários, `scripts/deploy-demetrius.sh`, guarda de atividade do backup,
> retenção e propagação do contexto de shutdown já têm código. `hardware` e
> `picoclaw-agent` também já foram retiradas das skills do workspace.
> A rodada atual corrige isolamento de memória/shell, confirmação do sono,
> orçamento/deadline, aliases/horários, origem de configuração e concorrência.
> Ver [dossiê e validação](../docs/internal-audit-2026-09-16.md).
> Pendências preservadas: `kuromatsu-docs`, políticas de sono por agente,
> S36–S38, ensaio de restore e medições no hardware-alvo.
> `depends_on`: S11, S17, S27, S30, S32, S33, S38.

Só o que **não** está entregue. Para o que já foi entregue e auditado, ver
`entregues/E0..E7,E10-*.md`. IDs (`FR-`, `ADR-`, `S`) nunca mudam de lugar por
estarem citados aqui — este arquivo só indexa, não é dono de nenhum capítulo.

> **2026-09-12 — André confirmou FR-013..020/AC-010-7..9** ("pode mandar
> brasa"): Gate da metodologia liberado, ADR-013..018 promovidas de
> `proposed` para `accepted`, os 8 capítulos novos (S09/S16/S17/S21/S27/S30/
> S33/S34) promovidos de `draft` para `confirmed` (mesmo critério já usado em
> S18/S29/S32: capítulo `confirmed` descreve desenho assentado, não implica
> que o código exista — os gaps reais listados abaixo continuam gaps reais).
> Onda 2 (código, Trilho B→A→C) iniciada. Todas as menções abaixo a
> "`proposed`"/"`draft` aguardando confirmação" referem-se ao estado **antes**
> desta confirmação — mantidas como estavam por serem histórico da sessão.

Auditoria mais recente: 2026-09-11, rodada 2 (E0-E7/E10 seguem válidos da
rodada 1). Esta rodada rodou **ao vivo, em paralelo com o `spec-writer`
rodada 2 e o `spec-verifier`** (auditoria read-only independente) — o
`spec-writer` terminou de formalizar **todo** o plano `demetrius` em spec
(ADR-013..018, FR-013..020, S09/S16/S17/S18/S21/S27/S29/S30/S32/S33/S34, S06
bumped) durante esta própria sessão, e este arquivo foi atualizado várias
vezes para acompanhar. O que resta abaixo é o resíduo real depois disso: uma
lacuna de sincronia em `spec-coverage.yaml`/`spec.manifest.yaml` (corrigida
por este agente), achados de precisão factual do `spec-verifier` em FR-020
(não corrigidos por mim — ver seção dedicada), e os entregáveis que já
estavam abertos antes do plano `demetrius` (E8, E9 parte 2).

---

## Entregáveis abertos

### E8 — Skill `kuromatsu-docs` (FR-009, ADR-010)

**Status**: não iniciado. `workspace/skills/kuromatsu-docs/` não existe no
repo (confirmado nesta auditoria). As três ACs de FR-009 seguem sem código:
skill descobrível via `find_skills`, um arquivo canônico por tópico, e
`git ls-files '*README*'` reduzido a só o `README.md` raiz — o que hoje
**não** é verdade (README raiz e `pkg/channels/README*.md` ainda descrevem o
launcher web e os 17 canais removidos no E2, como o próprio commit `cb895bec`
já registrou).

### E9 parte 2 — Bridge do modo dormir (FR-010, ADR-006/008/018)

**Status**: parcialmente entregue, e o escopo da parte 2 **cresceu nesta
rodada** (ADR-018, `proposed`). `pkg/sleep` (núcleo: janela, triagem com teto
de tokens, aplicação em `MEMORY.md`, relatório) existe e está testado
isoladamente (commit `4ff87eb8`, "E9, part 1/2" — 19 testes verdes). **Falta**:

- `pkg/agent/sleep_bridge.go` — não existe (confirmado via busca no repo).
  Espelharia `pkg/agent/evolution_bridge.go`, que existe e serve de modelo.
- O bloco de config `sleep` (`config.Sleep`) — não confirmado existente.
- Os adapters reais de `SessionSource`/`ChatFunc` ligados à fallback chain do
  agente (hoje só há fakes nos testes de `pkg/sleep`).
- **Novo nesta rodada (ADR-018/AC-010-7..9, `draft`)**: `ValidateSleep` em
  `LoadConfig` — trava que exige `sleep.unconscious_model` configurado e
  não-nativo antes do sono rodar, e a mesma trava para `evolution.model`. Sem
  isso, o Bonsai local continuaria podendo "sonhar com os próprios pesos",
  exatamente o risco que a ADR-018 endereça (orçamento de CPU do e2-micro,
  integridade do `MEMORY.md`, RAM).

Sem a parte 2, **AC-010-1 e AC-010-4 não têm onde rodar em produção** e
**AC-010-7..9 não têm nenhum código ainda** (são `draft`, aguardando
confirmação do André antes de implementar — Gate da metodologia).
AC-010-2/3/5/6 estão cobertas dentro de `pkg/sleep` isoladamente, mas não
fim-a-fim.

### Gate de desempenho do E7 (NFR-002, S39) — fechado com benchmark real dos Trilhos A/B/C (2026-09-13)

**Status**: `S39` passou de `tbd` para `confirmed`
(`06-infrastructure/39-baseline-benchmarks.md`) após o deploy dos Trilhos
A/B/C na `demetrius` e uma rodada de benchmark quantitativo com valores
reais (não estimados). Achados principais: cache de prefixo do KV
confirmado dentro do turno (98,3% hit, ~40× de aceleração do prefill) e
**entre turnos de heartbeat** (88,0% hit, ~11×, achado novo não previsto
no plano original); `n_threads=4` trava a VM com zero conclusões em 14+
min (valida a correção `NThreads=min(NumCPU,4)` do ADR-015); regressão
`PICOCLAW_*`→`KUROMATSU_*` confirmada em produção; memória dentro da meta
(NFR-001/008) com folga. **Dois itens abertos ficaram para decisão do
André** (não corrigidos nesta sessão):

1. **Shutdown gracioso não cancela inferência em voo** — `systemctl stop`
   com um turno em andamento deixa o processo vivo até o timeout de 90s
   do systemd, que então manda `SIGKILL`; depois disso, `systemctl start`
   exige `reset-failed` explícito antes de funcionar. Precisa de um
   ADR/tarefa nova para propagar o cancelamento do contexto de shutdown
   até o turno em execução (ou aceitar o comportamento e documentar).
2. **Overhead de framework da janela `heartbeat` pode estar acima da meta**
   (NFR-007, ≤300 tokens) — medição de campo mostrou 2603 tokens totais
   (system+user, sem histórico), bem mais do que os ~1,0-1,2k tokens de
   workspace do usuário explicariam. Precisa do teste isolado
   `prompt_size_test`/A9 para confirmar se é o modo `compact` não tão
   compacto quanto o desenho pretendia, ou outra causa.

**Ainda não medido** (mantém a recomendação do G0): piso "burst pleno" numa
VM `demetrius` fria (sem atividade prévia na sessão) — o benchmark de
2026-09-13 mostrou uma *recuperação parcial* dos créditos ao longo de
~30-40 min (do piso mais severo de ~0,44 tok/s para ~1,2-1,7 tok/s), mas
não isolou o platô "totalmente frio". Ver S39 para os números completos e
a metodologia.

---

## Plano `demetrius` — formalizado em spec nesta sessão (histórico + o que resta)

Existe um plano de refatoração aprovado em 2026-09-11 (fora do repositório,
`C:\Users\pc\.claude\plans\quero-refatorar-esse-projeto-twinkly-papert.md`)
que substitui o antigo "E8-E10" por cinco trilhos novos (A: janelas de
foco/reflexos; B: cache de prefixo do KV; C: motor de estados
`runstate`/guarda de memória/telemetria/bridge do sono; D: infra/systemd/trim
de SO; 0: spikes e migração de alvo) e move o alvo de execução principal da
Oracle para a VM `demetrius`. **O `spec-writer` formalizou o plano inteiro em
spec durante esta própria sessão** (em várias ondas, capturadas ao vivo por
este agente — ver o handoff para o histórico de descoberta). Estado final
confirmado nesta auditoria:

- **ADR-013..018** (`07-engineering/adr/`) — todas `proposed`, com contexto,
  decisão, alternativas e consequências reais (números de medição incluídos,
  ex. ADR-013 traz a tabela de levantamento da `demetrius`). ADR-006 marcada
  `superseded_by: ADR-018` e ADR-003 marcada `superseded_by: ADR-015` (ambas
  parciais — só as cláusulas específicas citadas em cada uma, o resto segue
  em vigor). **Correção deste agente**: nenhuma das duas tinha incrementado
  `version` no frontmatter apesar do conteúdo ter mudado de fato (blockquote
  de supersessão inteiro) — achado do `spec-verifier`, confirmado e corrigido
  por mim (ADR-003 v1→2, ADR-006 v2→3).
- **FR-013..020** (`02-business-design/11-requirements.md`) — `draft`, com
  ACs completas (AC-013-1 a AC-020-7). FR-010 ganhou AC-010-7..9 (ver E9
  parte 2 acima).
- **Capítulos novos, todos com conteúdo real (não esqueleto)**: S09
  (`02-business-design/09-state-machines.md`), S16
  (`03-system-design/16-system-flow.md`), S17
  (`03-system-design/17-failure-semantics.md`), S21
  (`04-data-design/21-cache-strategy.md`), S27
  (`06-infrastructure/27-security.md`), S30
  (`06-infrastructure/30-scheduling.md`), S33
  (`06-infrastructure/33-backup.md` — **declara Q2 de S06 resolvida**, ver
  tabela abaixo), S34 (`06-infrastructure/34-observability.md`).
- **Capítulos existentes com bump para o alvo `demetrius`**: S06
  (`01-business/06-assumptions.md`, v3→4 — ganhou **R7**, o risco de
  créditos de burst do e2-micro que 4 ADRs já citavam sem existir, e **R8**,
  variação do host físico por boot; A1/A2 atualizados com o novo alvo), S18
  (`03-system-design/18-ai-component.md`, v3→4 — seção "Runtime v2": cache
  de prefixo, prompt/schema compactos, correção do bug de `n_threads` fixo
  em 4, núcleos de janela em RAM), S29 (`06-infrastructure/29-nfr.md`,
  v3→4 — NFR-002 desdobrada em burst×piso, +NFR-006..009), S32
  (`06-infrastructure/32-deployment.md`, v4→5 — seção "Deploy binário direto
  na demetrius", tabela de Ambientes atualizada).
- **`spec-coverage.yaml`** — todos os itens acima sincronizados como
  `draft`/`confirmed` com notas reais (conferido por último nesta sessão).
  **Correção adicional deste agente**: `S11` continuava `status: confirmed`
  sem nota, mas `11-requirements.md` já estava `draft` (v5) desde a rodada 1
  — achado do `spec-verifier`, confirmado e corrigido por mim (agora `draft`
  com nota explicando o motivo).
- **`spec.manifest.yaml`** — **gap encontrado e corrigido por este agente**:
  S27/S30/S33/S34 tinham arquivo e `status` corretos em `spec-coverage.yaml`
  mas não apareciam no manifest (que é o índice item→arquivo para agentes) —
  adicionadas as 4 entradas faltantes, no mesmo formato das demais.

**Atualização 2026-09-12**: o Gate foi liberado — André confirmou FR-013..020
("pode mandar brasa"). Onda 2 (código, Trilho B→A→C) já começou.

### O que ainda falta de verdade (não é invenção minha — achados do `spec-verifier`, não corrigidos)

O `spec-verifier` (auditoria read-only independente, rodando em paralelo)
encontrou divergências entre o que `FR-020`/`ADR-013` *descrevem* e o que
está *de fato* implementado em `deploy/`/`TASKS.md` — não corrigi nenhuma
delas eu mesmo (é conteúdo de FR/AC, mais natural o `spec-writer` decidir a
frase exata; risco de colisão num arquivo muito ativo nesta sessão).
Conferido por mim, diretamente, antes de escrever esta seção — todas ainda
presentes em `11-requirements.md` no momento deste handoff:

- **AC-020-7 cita o arquivo errado**: diz `/etc/sysctl.d/90-kuromatsu.conf`;
  o artefato real (já versionado, citado pelo próprio S32 novo) é
  `deploy/sysctl/99-z-kuromatsu.conf` (nome deliberado para vencer por ordem
  lexicográfica um `99-servidor.conf` legado da VM). O mesmo AC também funde
  o THP `madvise` num só lugar com o sysctl, mas na implementação real é um
  `systemd` oneshot separado (`deploy/systemd/kuromatsu-thp-madvise.service`).
- **AC-020-3/4/6 descrevem como já pronto o que ainda está no `Inbox` do
  `TASKS.md`**: CI ainda não publica os binários crus
  (`kuromatsu-native-linux-amd64`/`nativebench-linux-amd64`) — só o
  `.tar.gz` da imagem; `scripts/deploy-demetrius.sh` (citado por AC-020-4 e
  pela ADR-013) não existe no repo; o gating do backup por `runstate ==
  Idle` (AC-020-6) tem um `TODO` explícito na própria unit dizendo que
  **não** está implementado ainda (o timer roda no horário e aceita colidir
  com uma inferência em andamento).
- Isto não é uma contradição interna da spec — é dessincronia entre trilhas
  paralelas (o `spec-writer` escreveu o *desenho* a partir do plano; o
  Trilho 0/D real na VM está em andamento, rastreado em `TASKS.md`, e ainda
  não chegou a esse ponto). Mas como ACs descrevem contrato de aceite, isso
  merece ajuste de fato: marcar essas 4 ACs como pendentes de implementação
  em vez de descrevê-las como comportamento já garantido, e trocar o nome do
  arquivo sysctl.

### Achados menores, não corrigidos (baixo risco, baixo impacto)

- **S25**: `status: confirmed` sem campo `file:` em `spec-coverage.yaml` —
  a convenção da skill pede `file` para itens `confirmed`. Achado do
  `spec-verifier`, pré-existente (não introduzido nesta rodada), baixa
  prioridade.

---

## Questões abertas herdadas (já em `spec-coverage.yaml`/S06, ainda sem dono fechado)

| Item | Onde está registrado | Situação |
|---|---|---|
| S36, S37, S38 | `spec-coverage.yaml` (`missing`) | Convenções de dev, tabela de env vars e estratégia de teste consolidada — não bloqueiam nada hoje, mas crescem junto com os trilhos A/B/C/D |

(Esta tabela encolheu bastante nesta rodada: Q1 e agora **Q2** — política de
backup, resolvida por `33-backup.md`, com a linha correspondente em S06
tachada e apontando para S33/gaps residuais (retenção, restore) — saíram por
estarem de fato resolvidas. S09/S16/S17/S21/S27/S30/S33/S34 também saíram:
todos tinham `missing`/`n/a` no início da sessão e hoje são `draft` com
conteúdo real e coverage sincronizado. Só as convenções de dev/env/teste
(S36-38) seguem realmente sem dono.)

## Resíduo de limpeza observado (não é um FR — é um achado, ainda não agido)

`workspace/skills/hardware/` e `workspace/skills/picoclaw-agent/` (skills do
agente, não o código Go de `pkg/tools/hardware` já removido no E2) ainda
existem em disco — confirmado de novo nesta rodada (`ls workspace/skills/`).
O plano de refatoração aprovado já atribui essa limpeza ao agente
`code-analyst`. Nenhuma ação tomada por este entregável.

## Decisão desta rodada — E9/E10 formalizados ou não em `entregues/`

A rodada 1 deixou como pergunta ao orquestrador se `entregues/E9-*.md` e
`entregues/E10-*.md` deveriam ser formalizados já, mesmo fora do recorte
original E0-E7, dado que ambos tinham evidência de código+teste. Decisão
tomada nesta rodada, dentro do escopo deste agente:

- **E10 (tool `sysmon`, commit `9d891ab9`) — formalizado agora**:
  `entregues/E10-tool-sysmon.md` criado. Critério do SOUL.md (FR referenciada
  + código que implementa + teste verde) está integralmente satisfeito, e o
  entregável é autocontido — não depende de nada do E9 nem do plano
  `demetrius` (ainda que FR-017/S27 venham a tocar `pkg/tools/sysmon.go` no
  futuro, isso não desfaz o que já foi entregue e testado agora). Adiar não
  reduzia risco nenhum, só um registro pronto ficando fora do índice.
- **E9 (`pkg/sleep`, commit `4ff87eb8`) — não formalizado**: só a parte 1
  está pronta, e a parte 2 **cresceu de escopo nesta própria rodada**
  (ADR-018/AC-010-7..9, ver seção acima) — criar `entregues/E9-*.md` agora
  documentaria uma entrega parcial como se fosse um capítulo fechado, e
  precisaria ser reaberto de qualquer forma quando a parte 2 chegar. Mantém-se
  a regra do SOUL.md: só um `entregues/E9-*.md` único, quando E9 fechar por
  completo.

`INDEX.md` já foi atualizado (`entregues/E0..E7,E10-*.md`) para refletir o
novo arquivo.
