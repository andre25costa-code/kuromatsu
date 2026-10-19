# Instalação, onboard, perfis, atualização e remoção

Fonte real: `scripts/install.sh`, `scripts/install.ps1`,
`cmd/kuromatsu/internal/onboard/`, `pkg/setup/`, `pkg/hwprofile/`,
`cmd/kuromatsu/internal/status/`, `pkg/updater/`,
`cmd/kuromatsu/internal/uninstall/`.

## Instaladores (`install.sh` e `install.ps1`)

```bash
curl -fsSL https://raw.githubusercontent.com/andre25costa-code/kuromatsu/main/scripts/install.sh | sh
```

```powershell
irm https://raw.githubusercontent.com/andre25costa-code/kuromatsu/main/scripts/install.ps1 | iex
```

Nenhum dos dois pede root/admin nem Go instalado. O que fazem:

1. **Detecção:** `install.sh` cobre Linux, macOS e Termux (`uname -s`/
   `uname -m`: `x86_64`, `arm64`, `armv7`, `armv6`, `riscv64`; no Termux só
   `aarch64`). `install.ps1` cobre Windows `AMD64` e `ARM64` (lê
   `PROCESSOR_ARCHITEW6432`, então um PowerShell 32 bits num Windows 64 bits
   pega a CPU real). Outra combinação: `unsupported OS/arch: ...`.
2. **Download por nome exato:** `kuromatsu_<Os>_<Arch>.tar.gz`
   (`kuromatsu_Windows_<Arch>.zip` no Windows) e o `checksums.txt` da última
   release. O sha256 do arquivo tem que bater com a linha de nome **exato**
   no `checksums.txt`; sem a linha, com hash malformado ou diferente, o
   instalador para com `checksum mismatch for <arquivo>`. Sem release
   publicada: `no release published yet; build from source (README) or set
   KUROMATSU_INSTALL_BASE_URL`.
3. **Destino** (gravação atômica: copia para um nome temporário e renomeia):
   - `KUROMATSU_INSTALL_DIR`, se definido;
   - Termux: `$PREFIX/bin`;
   - Linux/macOS: `~/.local/bin`;
   - Windows: `%LOCALAPPDATA%\Kuromatsu\bin`.
   Antes de gravar, confere se há 3× o tamanho do arquivo livre no destino.
4. **PATH com marcador:** no Unix, acrescenta
   `export PATH="<dir>:$PATH" # added by kuromatsu installer` ao
   `~/.profile` (e ao `.bash_profile`/`.bash_login`/`.bashrc`/`.zshrc` que
   existirem; `.zshrc` também se o shell for zsh), só se o diretório ainda
   não estiver no PATH. Fish: `fish_add_path <dir>` à mão. No Windows,
   acrescenta o diretório ao PATH do usuário (registro `HKCU\Environment`,
   preservando o tipo do valor) e grava o marcador
   `.kuromatsu-path-added` no diretório; só com esse marcador o
   `uninstall` tira a entrada do PATH depois.
5. **Onboard:** com terminal, roda `kuromatsu onboard </dev/tty` (sob
   `curl | sh` o stdin é o script, então o assistente lê o terminal); sem
   terminal (container, CI), roda `kuromatsu onboard --non-interactive`.
   Código 2 do onboard (setup não concluído) vira só o aviso `warning: setup
   skipped; run 'kuromatsu onboard' later` e a instalação conta como
   sucesso; outro código diferente de 0 é falha.

Variáveis dos dois instaladores:

| Variável | Efeito |
|---|---|
| `KUROMATSU_INSTALL_BASE_URL` | De onde baixar (`http://`, `https://` ou `file://`); default: a última release do GitHub |
| `KUROMATSU_INSTALL_DIR` | Diretório de instalação |
| `KUROMATSU_INSTALL_NO_ONBOARD` | `1` pula o onboard (rode `kuromatsu onboard` depois) |

Os instaladores instalam sempre o binário Go puro, sem o modelo nativo. O
nativo vem do build do fonte ou do arquivo nativo da release (abaixo).

## `kuromatsu onboard`

Cria a config (`config.json` + `.security.yml`) e o workspace (o que é
criado e o que o `--force` restaura: [workspace.md](workspace.md)). Também
cria o token da web UI quando falta.

| Flag | Efeito |
|---|---|
| `--non-interactive` | Nunca pergunta (instaladores, Docker, sem TTY); não combina com `--enc` |
| `--profile <ultra-light\|light\|balanced\|performance\|auto>` | Aplica um perfil de hardware a uma config existente (`auto` = o detectado) |
| `--enc` | Liga a criptografia de credenciais (gera chave SSH e pede senha) |
| `--force` | Sobrescreve os arquivos do workspace com os modelos |

### Assistente interativo (6 passos)

Num terminal (stdin e stdout TTY), sem `--non-interactive` e sem `--enc`:

1. **Idioma:** grava a linha `- Idioma preferido: pt-BR` ou `- Preferred
   language: en` no `USER.md` do workspace (não existe campo de idioma no
   config).
2. **Provedor de modelo**, com teste de conexão (`ProbeModels` em
   `pkg/setup`): Bearer para OpenAI-compatível e OpenRouter, nada para
   Ollama, `x-api-key` + `anthropic-version` para Anthropic,
   `x-goog-api-key` para Gemini.
3. **Canal Telegram:** token no `.security.yml` e o ID do dono em
   `allow_from` (ver [channels.md](channels.md)).
4. **Perfil de hardware** (abaixo): numa config nova, Enter usa o
   detectado; numa existente, Enter mantém o atual.
5. **Fuso:** o default é o `timezone` atual, senão `TZ`, senão o fuso lido
   de `/etc/localtime` (alvo do symlink depois de `zoneinfo/`) ou
   `/etc/timezone`. No Windows o fuso não é detectado. Sem fuso conhecido,
   Enter deixa `timezone` vazio (= fuso do sistema).
6. **Predefinição de permissões** (`seguro`, `assistente`, `avancado` —
   tabela em [config.md](config.md#predefinições-de-permissões)). Numa
   config nova, Enter escolhe `assistente` (exec desligado).

Grava uma vez, depois de um resumo. `q` sai sem gravar nada (assim como
recusar o resumo ou o fim da entrada) e sai com **código 2** (setup não
concluído); erro sai com 1. Na reexecução, cada passo oferece `[0] Manter
atual` como default (modelo, canal, perfil e permissões): Enter em tudo
não muda nenhuma dessas chaves.

### Sem perguntas (`--non-interactive`)

Sem TTY, com `--non-interactive` ou com `--enc`, roda o fluxo sem
perguntas: cria o que falta e mantém o `DefaultConfig()` (= predefinição
`avancado`: exec ligado só em canais internos). É o que os instaladores sem
terminal e o `docker/entrypoint.sh` usam (ver [deploy.md](deploy.md)).

## Perfis de hardware

O onboard de uma config nova detecta o hardware e aplica o perfil
correspondente (linha `Hardware: <RAM>, <N> CPUs → profile <nome>
(applied)`). O perfil é escolhido pela RAM efetiva (a menor entre a RAM
total e o limite do cgroup) e pelo número de CPUs, nesta ordem:

| Perfil | Condição |
|---|---|
| Ultra-Light | RAM < 768 MiB |
| Light | RAM < 2048 MiB |
| Balanced | RAM < 8192 MiB, ou menos de 4 CPUs |
| Performance | o resto |
| (RAM não detectada) | Light |

Os valores que cada perfil grava, e o limite de contexto do modelo nativo
nos perfis pequenos, estão em
[config.md](config.md#perfis-de-hardware).

`kuromatsu status` mostra, além de config, workspace e chaves:

- `Hardware`: RAM total e disponível, limite do cgroup, CPUs e arquitetura;
- `Profile`: o perfil detectado e a qual perfil a config corresponde
  (`custom` se nenhum bater; um campo zerado conta como o default);
- `Native CPU`: se a CPU tem as instruções do build nativo (`n/a` num build
  Go puro).

**Guarda de CPU do nativo:** o build nativo exige AVX2+FMA (x86-64) ou
dotprod+fp16 (arm64). Numa CPU sem isso, o modelo nativo não é ligado como
default nem como fallback, uma carga explícita é recusada com erro, e o
gateway registra um aviso no boot.

## Build a partir do fonte

Go puro (sem o modelo nativo):

```bash
make build        # build/kuromatsu
```

Nativo (llama.cpp in-process, cgo; ver [models.md](models.md)):

```bash
git submodule update --init      # llama.cpp
make build-native-x86-64         # build/kuromatsu-native-linux-amd64, baseline AVX2
make build-native                # alternativa: otimizado para a CPU desta máquina
bash scripts/download-model.sh   # models/Bonsai-1.7B-Q1_0.gguf
```

O `.gguf` é procurado em `$KUROMATSU_MODELS_DIR/<id>.gguf`,
`<home>/models/<id>.gguf` e `./models/<id>.gguf`, nessa ordem.

**Arquivo nativo da release:** cada release pode trazer também
`kuromatsu-native_Linux_x86_64.tar.gz` (só linux/amd64), listado no mesmo
`checksums.txt`. Ele é ligado dinamicamente à glibc e à libstdc++ de um
Debian bookworm (ou mais novo); numa distro mais antiga o binário não
abre. Os instaladores não o usam: baixe e extraia à mão.

## `kuromatsu update`

Baixa a última release (ou a de `--url`) e troca o próprio executável,
guardando o anterior como `<executável>.old`.

- Escolhe o arquivo pelo **nome exato**
  `kuromatsu_<Os>_<Arch>[v<GOARM>].<tar.gz|zip>` (`amd64` vira `x86_64`,
  `386` vira `i386`, ARM sem `GOARM` conhecido assume 7). `--platform` e
  `--arch` trocam o alvo.
- Um binário **nativo** só aceita `kuromatsu-native_Linux_x86_64.tar.gz`:
  outro alvo é recusado antes de qualquer download, e uma release sem o
  arquivo nativo falha com a dica de rodar `make build-native-x86-64`. Um
  binário Go puro nunca escolhe o nativo.
- **Checksum fail-closed:** sem sha256 válido para o arquivo, ou com hash
  diferente, nada é trocado (`refusing to update` / `checksum mismatch`).
- Não compara versões: roda sempre e reinstala a última release, mesmo que
  seja a versão atual.

## `kuromatsu uninstall [--purge] [--force] [--yes]`

Sem flags, remove o binário instalado pelo instalador e as entradas de
PATH que ele acrescentou (as linhas com o marcador no Unix; no Windows, só
se o marcador `.kuromatsu-path-added` existir). O diretório de dados fica.

- Recusa rodar com o gateway ativo (`gateway is running (PID <n>); stop it
  first`).
- Binário em diretório de sistema (`/usr/bin`, `/usr/local/bin`, `/bin`,
  `/sbin`), em `/opt/` ou `/snap/`, ou num diretório sem permissão de
  escrita: fica, com a dica de remover pelo gerenciador de pacotes.
- Pede confirmação; sem terminal, exige `--yes` (`-y`).
- `--purge`: **primeiro** grava `kuromatsu-backup-<data>.zip` no seu
  diretório home (confere espaço livre antes) e só depois apaga o diretório
  de dados. Se o backup falhar, nada muda. Recusa: caminho relativo ou `.`,
  raiz do disco, o próprio home (ou um diretório que contém o home, também
  via symlink), um symlink, e um diretório sem `config.json` nem
  `.security.yml`. Entradas que o Kuromatsu não cria fazem o purge parar,
  a menos que `--force` seja dado.

O que o uninstall **não** apaga: o diretório de dados sem `--purge`; o zip
de backup; a chave de criptografia de credenciais (`--enc`), que é
avisada; um workspace configurado fora do diretório de dados; um binário de
gerenciador de pacotes.
