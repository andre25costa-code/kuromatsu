# Isolamento de processos filhos

Fonte real: `pkg/isolation/`.

`pkg/isolation` isola, no nível de processo, os processos **filhos**
lançados pelo Kuromatsu. **Não** isola o processo principal do Kuromatsu em
si.

## Escopo (caminhos que passam por aqui)

- tool `exec`
- providers de CLI (`claude-cli`, `codex-cli`)
- hooks de processo
- servidores MCP `stdio`

Todo caminho que gera subprocesso deve reusar esses helpers em vez de chamar
`cmd.Start`/`cmd.Run` direto.

## Modelo em uma frase

O processo principal do Kuromatsu roda no ambiente host normalmente; todo
processo filho passa primeiro pelo caminho de startup compartilhado de
`pkg/isolation`, que aplica isolamento específico da plataforma conforme a
config.

## Arquitetura (4 camadas)

1. **Config**: lê `config.Config.Isolation`, injeta via `isolation.Configure(cfg)`.
2. **Layout de instância**: resolve `config.GetHome()`, prepara diretórios da
   instância, monta o ambiente de usuário do runtime.
3. **Backend de plataforma**: Linux usa `bwrap`; Windows usa token
   restrito + integridade baixa + `Job Object`; outras plataformas não têm
   implementação.
4. **Startup unificado**: `PrepareCommand(cmd)`, `Start(cmd)`, `Run(cmd)`.

## Config

```json
{ "isolation": { "enabled": false, "expose_paths": [] } }
```

- `enabled` (default `false`).
- `expose_paths`: expõe caminhos do host dentro do ambiente isolado — só
  importa com `enabled: true`, e só funciona no Linux hoje.

```json
{
  "isolation": {
    "enabled": true,
    "expose_paths": [
      { "source": "/opt/toolchains/go", "target": "/opt/toolchains/go", "mode": "ro" },
      { "source": "/data/shared-assets", "target": "/opt/kuromatsu-instance-a/workspace/assets", "mode": "rw" }
    ]
  }
}
```

Regras: `source` é caminho no host; `target` é o caminho dentro do
isolamento (default = `source` se vazio); `mode` é `ro` ou `rw`; só pode
existir uma regra final por `target` (config carregada depois sobrescreve a
anterior para o mesmo `target`). No Windows, `expose_paths` configurado deve
falhar o startup em vez de fingir que funcionou.

## Diretórios da instância

Raiz segue `config.GetHome()` (`KUROMATSU_HOME` se setado, senão
`.kuromatsu` no home do usuário). Diretórios padrão: raiz da instância,
`skills`, `logs`, `cache`, `state`, `runtime-user-env` (+ no Windows,
`runtime-user-env/AppData/{Roaming,Local}`). `workspace` vem de
`cfg.WorkspacePath()` quando configurado. Se `GetHome()` cair no fallback
`.` com isolamento habilitado, o startup deve falhar.

## Redirecionamento de ambiente do usuário

Processo filho isolado recebe variáveis redirecionadas para dentro de
`runtime-user-env`:

- Linux: `HOME`, `TMPDIR`, `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME`.
- Windows: `USERPROFILE`, `HOME`, `TEMP`, `TMP`, `APPDATA`, `LOCALAPPDATA`.

## Comportamento por plataforma

**Linux** — depende de `bwrap` (bubblewrap); sem fallback automático se
ausente (`apt/dnf/yum install bubblewrap`, `pacman -S bubblewrap`,
`apk add bubblewrap`). Dá visão mínima de filesystem, namespace `ipc`,
montagens `source→target` ro/rw. Montagens default: raiz da instância +
caminhos mínimos de sistema (`/usr`, `/bin`, `/lib`, `/lib64`,
`/etc/resolv.conf`) + o caminho do executável/diretório/cwd/argumentos
absolutos quando necessário. Desabilitar aumenta o risco de o processo
filho acessar/modificar mais arquivos do host do que deveria.

**Windows** — token primário restrito, nível de integridade baixo, `Job
Object`, ambiente redirecionado. Não implementa remapeamento real de
filesystem `source→target`; `expose_paths` configurado falha o startup.

**macOS e outras plataformas** — não implementado. Isolamento habilitado
explicitamente numa plataforma sem suporte deve surgir como configuração
não suportada, não como sucesso fingido.

## Log e depuração

Com isolamento habilitado, o Kuromatsu loga o plano gerado: `linux
isolation mount plan` (Linux), `windows isolation access rules` (Windows).
Se suspeitar que o isolamento não está efetivo, checar se caminhos
inesperados do host aparecem nesses logs.

## Relação com `restrict_to_workspace`

`restrict_to_workspace` limita os caminhos que o **agente** normalmente
pode acessar; `pkg/isolation` limita o que um **processo filho** consegue
ver e onde seu ambiente de usuário aponta. São complementares, um não
substitui o outro.

## Limites atuais

- Linux usa `bwrap`, não um runtime de isolamento próprio in-process.
- Linux não habilita namespace `pid` dedicado por default.
- Windows ainda não implementa ACL completa de host para todo caminho
  permitido/negado.
- macOS não implementado.
- O desenho atual isola processos filhos, não o processo principal do
  Kuromatsu.

## Ordem de leitura sugerida (para quem é novo neste código)

`pkg/config/config.go` → `pkg/isolation/runtime.go` →
`pkg/isolation/platform_linux.go` → `pkg/isolation/platform_windows.go` →
pontos de chamada: `pkg/tools/shell.go`, `pkg/providers/*.go`,
`pkg/agent/hook_process.go`, `pkg/mcp/manager.go`.
