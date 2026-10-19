# Interface web local (`/ui`)

Fonte real: `pkg/webui/`, `cmd/kuromatsu/internal/ui/`, `pkg/setup/`.

Página de configuração para quem não quer editar JSON. O gateway a serve
no mesmo listener dos webhooks (`gateway.host`/`gateway.port`, default
`localhost:18790`): a página em `/ui/` (`/ui` redireciona) e a API JSON em
`/api/ui/`. As chaves `webui.*` (default, env, onde moram) estão em
[config.md](config.md#web-ui-webui).

## Telas

1. **Assistente:** edita `SOUL.md`, `USER.md` e `AGENT.md` do workspace.
2. **Modelos:** lista, adiciona/edita (com teste de conexão) e escolhe o
   modelo default. Chave em branco mantém a atual; chaves nunca voltam nas
   respostas.
3. **Canais e permissões:** `allow_from` de cada canal e a predefinição de
   permissões (ver [config.md](config.md#predefinições-de-permissões)).
4. **Skills:** lista, instala (repositório `owner/name`) e remove.
5. **Automações:** cria a partir dos modelos, edita, pausa, retoma, roda
   agora, histórico e remoção (ver [automations.md](automations.md)).
6. **Heartbeat:** liga/desliga, intervalo (mínimo 5 min) e o
   `HEARTBEAT.md`.
7. **Status e consumo:** versão, modelo, resumo da config, canais,
   hardware, perfil (detectado e o da config) e o relatório de telemetria.

Os arquivos do workspace aceitos pela API são só `SOUL.md`, `USER.md`,
`AGENT.md` e `HEARTBEAT.md`, até 256 KB.

## Token

- 32 bytes aleatórios em hex, guardado só no `.security.yml`
  (`webui.token`; `KUROMATSU_WEBUI_TOKEN` sobrescreve e nunca é gravado).
- Nasce só no `kuromatsu onboard` (quando falta) ou no `kuromatsu ui token`,
  que cria se faltar e imprime. O gateway nunca grava o config: sem token
  ele não monta a UI e registra `web UI disabled: no token; run 'kuromatsu
  ui token'`. Um token criado com o gateway rodando só vale depois de
  reiniciá-lo.
- `kuromatsu ui open` só **imprime** o endereço
  (`http://localhost:<gateway.port>/ui/`) e avisa se `webui.enabled` está
  `false`; não abre o navegador.

## Modelo de segurança

Em ordem, para cada requisição:

1. **Só loopback** (com `allow_remote: false`): o `RemoteAddr` tem que ser
   loopback (`::ffff:127.0.0.1` conta). Qualquer cabeçalho de proxy
   (`X-Forwarded-For`, `X-Forwarded-Host`, `X-Forwarded-Proto`, `Forwarded`,
   `X-Real-IP`, `CF-Connecting-IP`, `True-Client-IP`) faz a requisição
   contar como remota. Resposta: 403.
2. **Lista de `Host`** (com `allow_remote: false`): só `localhost`,
   `127.0.0.1` e `[::1]`, em qualquer porta (então um túnel numa outra
   porta local funciona). Outro nome: 403, o que barra DNS rebinding.
3. **Cabeçalhos:** CSP restrita a `'self'` (imagens também `data:`), `X-Frame-Options: DENY`,
   `nosniff`, `Referrer-Policy: same-origin`; a API responde
   `Cache-Control: no-store`. Caminhos não canônicos (`..`, `//`) dão 404.
4. **Autenticação:** `POST /api/ui/login {"token": ...}` (com `Origin`
   exato) cria a sessão: cookie `km_session` (HttpOnly, SameSite=Strict,
   12 h deslizantes, em memória: reiniciar o gateway pede novo login) e
   devolve o token CSRF. Scripts usam `Authorization: Bearer <token>`.
5. **Limite de login:** 5 erros em 1 min por IP dão 429 até o minuto
   acabar. Em loopback todos os processos locais dividem esse limite; o
   Bearer não é limitado.
6. **CSRF/Origin:** com o cookie de sessão, toda mudança exige `Origin`
   (ou, sem ele, `Referer`) igual a `http://<Host>` e o cabeçalho
   `X-CSRF-Token` da sessão. Sem CORS: um preflight `OPTIONS` sempre falha.

**Equivale a um shell.** O token e uma sessão logada podem ligar a execução
de comandos (tela de permissões); trate o token como senha. O cookie
`km_session` não é isolado por porta: outros apps web locais abertos no
mesmo navegador (`http://localhost:<outra porta>`) podem recebê-lo (o
cookie do CSRF leva a porta no nome). Em máquina que roda apps web locais
não confiáveis, deixe a UI desligada ou saia (Sair) ao terminar. Em máquina
compartilhada, use `webui.enabled: false`.

## Gravações

Cada mudança de config carrega o arquivo como está (`LoadConfigForEdit`,
sem valores do ambiente), aplica a mudança, valida com
`config.ValidateForSave` (sem efeito no processo), grava e pede o mesmo
reload do `/reload`. Inválido → 422 e nada gravado. Se o reload não puder
começar (por exemplo, outro já em curso), a resposta é 202 com aviso: a
mudança está salva e vale no próximo reload. `allow_from` vazio num canal
ligado é recusado (rejeitaria todos). As gravações pela UI e o `/config
set` do chat usam a mesma trava, então não se atropelam.

## Acesso de outra máquina

Use um túnel SSH, que chega ao gateway como loopback e com `Host`
`localhost`:

```bash
ssh -L 8080:localhost:18790 <your-host>
# no navegador local: http://localhost:8080/ui/
```

Um túnel que não acrescenta cabeçalhos (`ssh -R`, `socat`) também chega
como loopback, e aí só o token protege: não exponha a porta inteira do
gateway na rede.

`webui.allow_remote: true` desliga **só** as checagens de loopback e de
`Host` (passos 1 e 2); token, `Origin`, CSRF e o limite de login continuam.
O gateway avisa no boot: `web UI reachable from the network over plain
HTTP; prefer an SSH tunnel`. Não há TLS: prefira o túnel.

## Exige reinício

`webui.enabled`, `webui.allow_remote` e o token são lidos no start do
gateway; o reload não os aplica (ver [config.md](config.md#web-ui-webui)).
