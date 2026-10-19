# Deploy em servidor (systemd, Docker, CI)

Fonte real: `deploy/`, `scripts/deploy.sh`, `docker/`,
`.github/workflows/build-native-image.yml`, `.github/workflows/release.yml`
(decisão de projeto: num servidor pequeno, o binário roda direto sob
systemd, sem Docker em runtime).

Instalar numa máquina pessoal (instaladores, onboard, `update`,
`uninstall`) é outro tópico: [install.md](install.md).

## Alvo de referência

Binário nativo direto sob systemd numa VM pequena (1 GB de RAM, x86-64 com
AVX2). Sem AVX2+FMA a CPU não roda o modelo nativo (guarda de CPU em
[install.md](install.md#perfis-de-hardware)).

## Binário: release nativa ou build do CI

- **Release:** o `kuromatsu-native_Linux_x86_64.tar.gz` (requisitos de
  sistema em [install.md](install.md#build-a-partir-do-fonte)) vem no `checksums.txt` da release.
  Depois de instalado, `kuromatsu update` nesse binário só aceita o arquivo
  nativo (detalhes em [install.md](install.md#kuromatsu-update)).
- **CI:** `.github/workflows/build-native-image.yml` builda a imagem
  cgo/nativellm **e** publica os binários crus
  (`kuromatsu-native-linux-amd64`, `nativebench-linux-amd64`) como
  artefato — deploy não depende de `docker load`, é `gh run download` +
  `scp` direto.

## Unit systemd (`deploy/systemd/kuromatsu.service`)

`Type=notify` + `WatchdogSec=120` (o Kuromatsu publica `sd_notify` via
`pkg/runstate` — ping de vida independente das transições de runstate, não
mata um agente genuinamente ocioso). `MemoryMax=900M`/`MemoryHigh=850M`
(orçamento de uma VM de 1 GB; o memguard ajusta o limite do Go dentro dele).
`Environment=KUROMATSU_HOME=...` e `KUROMATSU_CONFIG=...` **declarados
explicitamente na unit** (evita ambiguidade sobre qual config está ativa).
Um `Environment=TZ=` na unit tem precedência sobre o campo `timezone` (ver
[config.md](config.md)). Endurecimento: `NoNewPrivileges`,
`ProtectSystem=full`, `ProtectHome`, `ReadWritePaths` restrito ao
`KUROMATSU_HOME`.

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
lexicográfica um `99-*.conf` legado):
`vm.swappiness=100` (zram comprimido é mais barato que descartar as
páginas mmap do modelo), `vm.page_cluster=0`, `vm.vfs_cache_pressure=50`.
`deploy/journald/00-kuromatsu.conf`: `SystemMaxUse=64M` (sem isso, journald
pode crescer sem limite num disco pequeno compartilhado com backup).

## Deploy de rotina

```bash
DEPLOY_HOST=<alias-ssh> bash scripts/deploy.sh <binário-kuromatsu> <binário-nativebench>
```

`DEPLOY_HOST` também pode vir de um `.env` na raiz do repositório. O
script copia os dois binários por `scp`, para o serviço, instala no
caminho do `ExecStart` da unit, inicia e mostra o `systemctl status`. Ele **não**
confere ociosidade, checksum nem `/health`, e não faz rollback: antes,
confira o `run/state` (sem `Inference`/`ToolExec`/`Dream`) e guarde o
binário e o `config.json` atuais; depois, faça a verificação abaixo.

Rollback: um binário antigo recusa um `config.json` com campo que ele não
conhece (`contains unknown field(s)`), e o binário novo pode ter gravado um
(ex.: `webui`). Restaure o binário anterior **e** o `config.json` de antes
do deploy.

## Web UI num servidor

A UI só responde em loopback. Para usá-la de outra máquina, abra um túnel
SSH (`ssh -L 8080:localhost:18790 <your-host>`) e acesse
`http://localhost:8080/ui/`; o modelo de segurança e o `allow_remote`
estão em [webui.md](webui.md).

## Docker

Três composes em `docker/`, todos com o diretório de dados montado de
`./data` (relativo a `docker/`) em `/root/.kuromatsu`:

| Arquivo | Imagem | Serviços (profiles) |
|---|---|---|
| `docker/docker-compose.yml` | `docker/Dockerfile` (Go puro) | `kuromatsu-agent` (`agent`), `kuromatsu-gateway` (`gateway`) |
| `docker/docker-compose.full.yml` | `docker/Dockerfile.full` (com Node/npm para servidores MCP) | os mesmos, mais um volume de cache do npm |
| `docker/docker-compose.native.yml` | `kuromatsu:native-amd64` (`docker/Dockerfile.native`) | `kuromatsu-native-agent`, `kuromatsu-native-gateway`; `mem_limit: 900m`, `../models` montado em `/models` (`KUROMATSU_MODELS_DIR`) |

```bash
docker compose -f docker/docker-compose.yml --profile gateway up
```

O gateway sobe pelo `docker/entrypoint.sh`. Na primeira execução (sem
`config.json` e sem workspace no volume) ele roda
`kuromatsu onboard --non-interactive`, imprime como adicionar um modelo
(`kuromatsu model add ...` via `docker compose exec`, ou editar o
`config.json`) e **sai**; suba de novo depois de configurar. Um
`config.json` já montado nunca é reescrito. Nas execuções seguintes, o
entrypoint apaga um arquivo de PID velho e roda `kuromatsu gateway`.

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
mesma config do servidor, sem mandar mensagem pros canais.
