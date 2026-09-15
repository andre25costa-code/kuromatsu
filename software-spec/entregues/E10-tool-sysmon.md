---
id: E10
title: Tool sysmon (observabilidade/controle), substituindo as tools de hardware Sipeed removidas
status: done
frs: [FR-011]
adrs: []
commits: [9d891ab9]
last_updated: 2026-09-11
---

# E10 — Tool sysmon

## O que foi entregue

`pkg/tools/sysmon.go` — tool determinística de observabilidade/controle do
processo e da máquina, ocupando o nicho que as tools de hardware Sipeed
(i2c/spi/serial, removidas no E2/ADR-007) deixaram. Um único commit
(`9d891ab9`), completo com código + config + testes:

- **Ações**: `mem` (lê o limite de cgroup v2/v1 quando roda em Docker,
  caindo para `/proc/meminfo` em bare metal — AC-011-1), `top` N processos
  por RSS (AC-011-3), load average, uso de disco, e `proc kill`/`proc renice`.
- **Portabilidade**: fontes específicas de Linux (`/proc`, cgroupfs, `Statfs`)
  em `sysmon_linux.go`; `sysmon_other.go` devolve "unsupported" em qualquer
  outro GOOS em vez de falhar o build.
- **Guardrail (BR-007/AC-011-2)**: ações destrutivas (`kill`/`renice`) são
  negadas a menos que `tools.sysmon.allow_destructive=true` — default
  `false`. Fiado como `SysmonToolConfig` em `pkg/config/config.go`, semeado
  desabilitado em `pkg/config/defaults.go`, e registrado em
  `pkg/agent/instance.go` atrás do gate padrão
  `cfg.Tools.IsToolEnabled("sysmon")` (a tool em si vem habilitada por
  padrão; só as ações destrutivas exigem o opt-in extra).
- **Testes**: 28 testes — lógica pura de dispatch/validação em
  `sysmon_test.go` (qualquer GOOS) e testes de parsing/integração
  Linux-only em `sysmon_linux_test.go` (contra arquivos de cgroup
  temporários e o `/proc` real). Mensagem do commit registra as suítes
  `pkg/tools`, `pkg/config` e `pkg/agent` verdes após a mudança, sem
  regressão.

## FRs cobertas

| FR | Descrição | ACs | Status |
|---|---|---|---|
| FR-011 | Tool sysmon | AC-011-1, AC-011-2, AC-011-3 | Verificado (código + config + 28 testes, ver commit) |

## Commits

| Commit | Mensagem | Diff |
|---|---|---|
| `9d891ab9` | feat(tools): add sysmon, replacing the removed Sipeed hardware tools (E10) | ver `git show --stat 9d891ab9` |

## Arquivos-chave (confirmados presentes nesta auditoria)

`pkg/tools/sysmon.go`, `pkg/tools/sysmon_linux.go`, `pkg/tools/sysmon_other.go`,
`pkg/tools/sysmon_test.go`, `pkg/tools/sysmon_linux_test.go`,
`pkg/config/config.go` (`SysmonToolConfig`), `pkg/config/defaults.go`
(seed desabilitado), `pkg/agent/instance.go` (registro via
`IsToolEnabled("sysmon")`).

## Como verificar

`git show --stat 9d891ab9`; `git log --oneline --all -- pkg/tools/sysmon.go`
(um único commit, confirmado nesta auditoria); `grep -rn "allow_destructive\|SysmonToolConfig" pkg/`.
Suíte de testes não re-executada nesta auditoria (ambiente sem toolchain Go
disponível na sessão) — evidência é a mensagem do próprio commit + presença
dos 2 arquivos de teste; se quiser confirmar "verde" de fato, rodar
`go test ./pkg/tools/... ./pkg/config/... ./pkg/agent/...` num ambiente com Go.

## Notas

Diferente de E0-E7 (que vieram numa auditoria delimitada a esse recorte),
este E10 foi deixado como dúvida em aberto pela rodada 1 desta auditoria
("formalizar agora ou só quando E9 completo?"). Decisão desta rodada:
formalizar E10 agora — critério do SOUL.md (FR referenciada + código que a
implementa + evidência de teste verde) está integralmente satisfeito, o
entregável é autocontido (não depende de nada do E9 nem do plano
`demetrius`), e mantê-lo fora de `entregues/` não reduzia nenhum risco, só
adiava um registro já pronto. FR-011 já estava recolhida em `<details>` no
S11 como "entregue (E10)" desde a rodada 1; este arquivo só formaliza a
mesma conclusão em `entregues/`, sem mudar nada em S11.

E9 (`pkg/sleep` + bridge) **não** ganha um `entregues/E9-*.md` nesta rodada
— ver `BACKLOG.md` para o porquê (só parte 1 está pronta; parte 2 cresceu de
escopo com a ADR-018 e ainda não tem código).
