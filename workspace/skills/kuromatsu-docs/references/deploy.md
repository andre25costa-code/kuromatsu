# Deploy em produção

Fonte real: `deploy/`, `scripts/deploy.sh`,
`.github/workflows/build-native-image.yml`, ADR-013.

## Alvo real

Produção roda num binário direto sob systemd (sem Docker em runtime) numa
VM pequena (referência: Google Cloud e2-micro, 1 GB de RAM).

## Unit systemd (`deploy/systemd/kuromatsu.service`)

`Type=notify` + `WatchdogSec=120` (o Kuromatsu publica `sd_notify` via
`pkg/runstate` — ping de vida independente das transições de runstate, não
mata um agente genuinamente ocioso). `MemoryMax=900M`/`MemoryHigh=850M`
(orçamento de 1GB, ADR-013/017). `Environment=KUROMATSU_HOME=...` e
`KUROMATSU_CONFIG=...` **declarados explicitamente na unit** (evita
ambiguidade sobre qual config está ativa). Endurecimento: `NoNewPrivileges`, `ProtectSystem=full`,
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
DEPLOY_HOST=<alias-ssh> bash scripts/deploy.sh <binário-kuromatsu> <binário-nativebench>
```

Confere se o serviço está ocioso (`run/state`) antes de trocar o binário,
para, troca, reinicia, confere `/health`. Ver o script para o passo a
passo exato (checagens de checksum, rollback).


## Backup

Faça backup de `config.json`, `.security.yml`, `workspace/` e `telemetry/`
(o `.gguf` não precisa: `scripts/download-model.sh` o reobtém). Rode o backup
só com o agente ocioso (`run/state` sem `Inference`/`ToolExec`/`Dream`) e
ensaie o restore — "backup existe" e "backup restaura" são afirmações
diferentes.

## Verificação pós-deploy (o que checar de verdade, não só "subiu")

`/health` (`{"status":"ok","state":{...}}`), `journalctl -u kuromatsu`
(sem `SIGKILL`, sem `context_length_exceeded` repetido),
`systemctl show kuromatsu -p NRestarts -p WatchdogTimestamp` (watchdog
batendo, sem restart inesperado), e — se a mudança tocou o motor nativo —
um `/show model`/`/show config` real via CLI do binário instalado, com a
mesma config de produção, sem mandar mensagem pros canais.
