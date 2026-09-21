---
id: S27
title: Segurança — superfície nova do runstate/sysmon e trim do SO
status: confirmed
version: 2
owner: André
last_updated: 2026-09-12
depends_on: [S06, S08, S13, S32]
---

# S27 — Segurança

> **Status**: `confirmed` — combina segurança herdada do PicoClaw (sem mudança) com a
> superfície nova do plano `demetrius` (ADR-013/016, `accepted`). Este item estava
> `missing` ("nova superfície a documentar depois — ações destrutivas do sysmon
> gated por config"). Isso deixou de ser um "depois": o `runstate` (S09) expõe um
> novo canal de estado (arquivo, socket, `/health`, ação `sysmon state`) e o trim do
> SO troca `fail2ban` por `nftables` — superfície suficiente para um capítulo próprio.

## Superfície nova (o que o plano `demetrius` adiciona)

| Superfície | O que expõe | Risco | Mitigação |
|---|---|---|---|
| `$KUROMATSU_HOME/run/state` (S09/ADR-016) | Bits ativos do `runstate` (`bits=… names=… since=…`) em texto plano, escrita atômica | Nenhum dado sensível — só nomes de bit (`Inference`, `Dream`…) e timestamp | Arquivo dentro de `ReadWritePaths=/var/lib/kuromatsu`, dono `kuromatsu`, sem permissão de outros usuários (`umask` do processo) |
| `sd_notify` (`$NOTIFY_SOCKET`, datagrama unix) | `STATUS=<names>`, `READY=1`/`STOPPING=1` | Nenhum — canal interno systemd↔processo, sem rede | Socket já restrito pelo systemd ao próprio serviço |
| Campo `state:{bits,names}` em `/health` | Estado operacional do agente (ocioso/inferindo/sonhando) | Baixo — não vaza conteúdo de conversa, só "o que o processo está fazendo agora" | `/health` já não exige token hoje; `/reload` (mutação) continua exigindo bearer token (`extractBearerToken`, `pkg/health/server.go:245-254`) — `/health` continua só leitura de estado, não ganha um endpoint de mutação novo |
| Ação `sysmon state` | Mesmo estado do `/health`, via tool interna do agente | Nenhum — é leitura, mesma classe de `sysmon mem`/`sysmon top` (já existentes, BR-007) | Sem mudança de gating: ações de leitura do `sysmon` continuam liberadas; `kill`/`renice` continuam negadas por padrão |
| Reflexo `action: exec` (FR-013) | Execução de comando por regex, sem passar pelo roteador/LLM | Poderia parecer um bypass de segurança | **Não é** — herda literalmente o mesmo gating de `pkg/tools` (deny patterns, `restrict`, timeouts) via `Tools.ExecuteWithContext`; AC-013-3 exige isso explicitamente |
| Backup cross-cloud (`kuromatsu-backup.timer` → `rsync` para o `kuro`) | Cópia de `/var/lib/kuromatsu` (config, workspace, sessões, memória) trafegando entre duas nuvens | Dados pessoais saindo da `demetrius` | **Push, não pull**: a `demetrius` inicia a conexão SSH de saída para o `kuro`; nenhuma porta nova é aberta em nenhum dos dois lados. Sem porta de entrada nova na `demetrius` — mesmo perfil de rede de hoje (Telegram por long-polling) |

## Segurança herdada (sem mudança nesta refatoração)

- **`.security.yml`** (`pkg/config/security.go`) e a redação de segredos em log
  continuam como estão — nenhum campo novo de credencial introduzido pelo plano
  `demetrius` (o `model_list` para um provider externo do modo dormir, ex. Ollama
  Cloud, usa o mesmo mecanismo de chave já existente para qualquer outro provider).
- **BR-007** (ações destrutivas do `sysmon` — `kill`/`renice` — negadas por padrão)
  não muda; a única ação nova (`state`) é somente leitura.
- **Allowlists de tools e `allow_read/write_paths`** (S15, `n/a` — usuário único)
  permanecem inalterados; o roteador de foco (S16) só reduz o *catálogo exposto ao
  modelo por turno*, nunca amplia o que uma tool tem permissão de fazer — a
  verificação de gating em si acontece no mesmo lugar de sempre (`pkg/tools`).
- **Docker desativado (não removido)** na `demetrius` (ADR-013) — reduz a superfície
  de ataque local (`docker.socket` não escuta), sem eliminar o rollback
  (`systemctl enable --now docker`, AC-020-5).

## Rede — nftables substitui o fail2ban

O trim do SO (Trilho 0/D) desativa o `fail2ban` (economiza ~20 MB de RSS) e o
substitui por uma regra nativa do `nftables` (`deploy/nftables/ssh-ratelimit.nft`,
já versionada no repo): rate-limit de até 6 novas conexões por IP a cada 60s na
porta 22, descartando o excedente **sem resposta** (sem RST) para não realimentar
scanners. Gate de aceite explícito no próprio arquivo — verificar `nft list ruleset`
e confirmar de outra máquina que tentativas excedentes de login passam a ser
descartadas **antes** de desativar o `fail2ban` (rollback: reativar o `fail2ban`,
nada foi removido, só desativado).

## Systemd — enrijecimento da unit (`deploy/systemd/kuromatsu.service`)

| Diretiva | Efeito |
|---|---|
| `NoNewPrivileges=true` | O processo não pode escalar privilégios mesmo que um binário SUID seja invocado |
| `ProtectSystem=full` + `ReadWritePaths=/var/lib/kuromatsu` | Sistema de arquivos somente-leitura fora do diretório de dados do agente |
| `ProtectHome=true` | Sem acesso a `/home`, `/root`, `/run/user` |
| `PrivateTmp=true` | `/tmp` isolado do resto do sistema |
| `ProtectKernelTunables=true` / `ProtectKernelModules=true` | Sem escrita em `/proc/sys`, `/sys`; sem carregar módulos de kernel |
| `ProtectControlGroups=true` | Sem escrita na própria hierarquia cgroup |
| `RestrictSUIDSGID=true` | O processo não pode criar arquivos SUID/SGID |
| `RestrictRealtime=true` | Sem agendamento de prioridade realtime (defesa contra DoS de CPU) |

Essas diretivas já estão no arquivo versionado em `deploy/systemd/kuromatsu.service` —
aplicadas no Trilho 0 da migração real (em andamento; resultados parciais em
`.claude/team/research/g0-medicao-real.md`), não é um plano futuro.

## Cross-referências

- **Estados que o `runstate` expõe**: S09. **Exposição observável completa**
  (`/health`, `sysmon state`, `sd_notify`): S34.
- **Decisão de arquitetura do alvo/trim**: ADR-013. **Motor de estados**: ADR-016.
- **Arquitetura de deploy completa** (systemd, sysctl, trim, backup): S32/S33.
- **Regra de negação de ações destrutivas do sysmon**: BR-007 (S08).
