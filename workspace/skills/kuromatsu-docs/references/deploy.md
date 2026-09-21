# Deploy em produção

Fonte real: `deploy/`, `scripts/deploy-demetrius.sh`,
`.github/workflows/build-native-image.yml`, ADR-013.

## Alvo real

Produção roda num binário direto sob systemd (sem Docker em runtime) numa
VM Google `demetrius` (e2-micro). A Oracle (`kuro`) é alvo de **backup**
cross-cloud, não roda inferência (steal de CPU alto demais nessa VM).

## Unit systemd (`deploy/systemd/kuromatsu.service`)

`Type=notify` + `WatchdogSec=120` (o Kuromatsu publica `sd_notify` via
`pkg/runstate` — ping de vida independente das transições de runstate, não
mata um agente genuinamente ocioso). `MemoryMax=900M`/`MemoryHigh=850M`
(orçamento de 1GB, ADR-013/017). `Environment=KUROMATSU_HOME=...` e
`KUROMATSU_CONFIG=...` **declarados explicitamente na unit** (evita
ambiguidade sobre qual config está ativa — achado CFG-03 do audit de
2026-09-16). Endurecimento: `NoNewPrivileges`, `ProtectSystem=full`,
`ProtectHome`, `ReadWritePaths` restrito ao `KUROMATSU_HOME`.

Encerramento gracioso: o `ctx` de shutdown do gateway é propagado até o
`abort_callback` do llama.cpp — um turno em andamento é cancelado (não
`SIGKILL`) quando o systemd manda parar. Handlers de heartbeat/cron
recebem esse mesmo `ctx` real desde a criação (não `context.Background()`),
senão o cancelamento nunca alcança um turno disparado por eles.

## Trim do SO

`deploy/scripts/trim-os.sh` desativa (nunca purga) serviços que competem
por RAM/CPU numa VM pequena: `google-cloud-ops-agent*`,
`google-osconfig-agent`, `exim4`, `rsyslog`, `haveged`, `fail2ban` (
substituído por rate-limit via nftables, `deploy/nftables/ssh-ratelimit.nft`),
Docker (`docker.socket`, `docker`, `containerd`). Mantidos de propósito:
`google-guest-agent` (chaves SSH via metadata), `unattended-upgrades`
(patches de segurança), `chrony` (hora certa — TLS e agendamento dependem
disso). Idempotente, seguro rodar de novo após reinstalação.

`deploy/sysctl/99-z-kuromatsu.conf` (nome escolhido pra vencer por ordem
lexicográfica qualquer `99-servidor.conf` legado da VM):
`vm.swappiness=100` (zram comprimido é mais barato que descartar as
páginas mmap do modelo), `vm.page_cluster=0`, `vm.vfs_cache_pressure=50`.
`deploy/journald/00-kuromatsu.conf`: `SystemMaxUse=64M` (sem isso, journald
pode crescer sem limite num disco de 30GB compartilhado com backup).

## CI e binários

`.github/workflows/build-native-image.yml` builda a imagem cgo/nativellm
**e** publica os binários crus (`kuromatsu-native-linux-amd64`,
`nativebench-linux-amd64`) como artefato — deploy não depende de
`docker load`, é `gh run download` + `scp` direto.

## Deploy de rotina

```bash
bash scripts/deploy-demetrius.sh <binário-kuromatsu> <binário-nativebench>
```

Confere se o serviço está ocioso (`run/state`) antes de trocar o binário,
para, troca, reinicia, confere `/health`. Ver o script para o passo a
passo exato (checagens de checksum, rollback).

Para uma correção sensível de config/binário em produção (não é a rotina
normal), o padrão usado no audit de 2026-09-16 foi um script Python
dedicado com checksum obrigatório, backup privado com rollback automático
em qualquer falha, e sonda de prontidão com retry —
`scripts/deploy-audit-demetrius.py` é o exemplo real; reusar esse padrão
(não o script em si) para qualquer futura correção do mesmo tipo, em vez
de editar arquivo em produção via SSH manual.

## Backup

`deploy/systemd/kuromatsu-backup.{service,timer}` +
`deploy/systemd/backup-to-kuro.sh`: `tar.zst` de `config.json`,
`.security.yml`, `workspace/`, `telemetry/` (não inclui `models/` — o
`.gguf` é reobtido por `scripts/download-model.sh`, não é dado do
usuário), via `rsync` pro `kuro`, gateado por `runstate` (só roda se o
agente não estiver em `Inference`/`ToolExec`/`Dream`). Retenção: 14
arquivos mais recentes no lado remoto.

**Pendência real, não resolvida**: nunca foi feito um ensaio completo de
restore ponta a ponta (extrair o `tar.zst`, subir uma instância a partir
dele, confirmar que funciona). O timer existe e a lógica de gate/retenção
está implementada, mas "backup existe" e "backup restaura" são afirmações
diferentes — a segunda não está comprovada.

## Verificação pós-deploy (o que checar de verdade, não só "subiu")

`/health` (`{"status":"ok","state":{...}}`), `journalctl -u kuromatsu`
(sem `SIGKILL`, sem `context_length_exceeded` repetido),
`systemctl show kuromatsu -p NRestarts -p WatchdogTimestamp` (watchdog
batendo, sem restart inesperado), e — se a mudança tocou o motor nativo —
um `/show model`/`/show config` real via CLI do binário instalado, com a
mesma config de produção, sem mandar mensagem pros canais.
