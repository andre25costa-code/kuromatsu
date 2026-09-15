---
id: ADR-019
title: Multi-agente real (agents.list/dispatch/pin), heartbeat e cron por agente, Ollama Cloud como fallback externo
status: accepted
version: 1
owner: André
last_updated: 2026-09-14
depends_on: [ADR-014, ADR-016, ADR-018]
---

# ADR-019 — Multi-agente, dispatch por agente e fallback externo (Trilho G)

## Contexto

O André pediu (comando `/architecture-designer-0.1.0`) três coisas relacionadas:

1. Modularidade real — poder criar e gerenciar múltiplos agentes na mesma
   instância do Kuromatsu, cada um com seu próprio workspace/modelo/tools. Caso
   de uso concreto: um agente `sensores`, isolado, só com o modelo local, num
   Raspberry Pi numa propriedade rural, controlando irrigação/automação via
   Telegram/WhatsApp, sem internet exceto para o modo dormir; e um agente
   `estudos` mais rico, turbinado por um provider externo quando necessário.
2. Um fallback de IA externa para uso interativo mais pesado (Ollama Cloud),
   sem vazar contexto/gastar tokens pagos à toa — heartbeats continuam sempre
   no modelo local.
3. Combinações flexíveis de provider por agente (local+ollama, ollama+ollama
   sem local, etc.).

Uma investigação read-only (3 agentes Explore + 1 agente Plan, todos lendo o
código real) encontrou que **grande parte disso já existia e funcionava**:
`agents.list`/`agents.dispatch.rules` (roteamento por canal/conta/chat),
múltiplos providers vivos simultaneamente por agente (`CandidateProviders`),
e fallback entre modelos/providers diferentes — só não estava documentado,
exemplificado, nem totalmente fiado (heartbeat/cron sempre usavam o agente
default; não havia comando de troca de agente). Esta ADR registra o que foi
adicionado para fechar essas lacunas, e o que foi deliberadamente deixado de
fora.

## Decisão

1. **Heartbeat por agente** (`Config.EffectiveHeartbeat`,
   `AgentLoop.ProcessHeartbeatForAgent`, `gateway.startHeartbeatServices`):
   um `heartbeat.HeartbeatService` por agente que tenha heartbeat habilitado
   (`agents.list[].heartbeat`, override campo-a-campo do bloco global).
   `agents.list` vazio (o caso comum) permanece byte-idêntico a antes —
   sempre exatamente um `HeartbeatService`, no agente implícito `main`.
2. **Cron por agente**: já funcionava sem mudança de código — um job cujo
   `payload.channel`/`payload.to` casa uma regra de `agents.dispatch.rules`
   já roda no agente certo, pelo mesmo caminho que canais normais usam
   (`ProcessDirectWithChannel` → `resolveMessageRoute`). Adicionado
   `payload.agent_id` como override explícito (bypassa as regras de
   dispatch) para um job que precise forçar um agente independente de
   canal/chat.
3. **Comando `/agent [id|auto]`**: pin sticky por chat, persistido em
   `$KUROMATSU_HOME/run/agent-pins.json` (sobrevive a restart — uma versão
   só-em-memória reverteria silenciosamente para o agente default em todo
   deploy/crash, achado real de uma revisão adversarial via `agy-bridge`
   antes de qualquer código ser escrito). Precedência explícita: **pin >
   agents.dispatch.rules > default**.
4. **Ollama Cloud como entrada `model_list`**: estruturalmente já era só
   mais um provider `openai_compat` (`provider: "ollama"`) — o cabeçalho
   `Authorization: Bearer <key>` já é enviado sempre que uma chave está
   configurada, independente do provider. Nenhuma mudança de código; só
   exemplo em `config.example.json` + testes (auth header e parsing de
   tool-calls, sem rede) + um teste de integração opt-in.
5. **Isolamento de prompt por provider: decisão de reaproveitar, não
   recriar.** Não existe (e não foi criada) nenhuma diferenciação de prompt
   por provider — o prompt de sistema é construído igual para todo
   provider, condicionado só por turn_profile/janela de foco. Para uma
   janela usada por um agente turbinado por provider externo, configurar
   conscientemente **dois knobs independentes**: `system_prompt.mode:
   "compact"` (economiza tokens de overhead do framework) e `memory`
   (`MemoryMode`: `core`/`off`) — este segundo é o que de fato controla se
   o `MEMORY.md` daquele agente é enviado a um provider pago de terceiros;
   os dois foram inicialmente conflados no desenho e corrigidos após a
   revisão adversarial identificar que "compact" não implica "sem
   memória".

## Alternativas rejeitadas

- **Segundo caminho de prompt dedicado por provider (padrão do
  `sleep_bridge.go`)**: rejeitada. Aquele padrão existe para uma tarefa
  fixa conhecida de antemão (consolidação de memória); o caso de um agente
  interativo genérico turbinado por provider externo precisa das mesmas
  tools/skills que o modelo local teria, só mais compacto — reaproveitar o
  mecanismo de foco/turn_profile já testado é mais simples e não duplica
  manutenção.
- **`/agent <id> <mensagem>` (roteamento de uma única mensagem, como
  `/foco`)**: rejeitada por ora. Diferente de foco (que só afeta como o
  prompt é montado dentro do mesmo `AgentInstance` já resolvido), trocar de
  agente muda QUAL `AgentInstance` roda o turno — decidido em
  `resolveMessageRoute`, chamado **antes** de `handleCommand`. Um comando
  que só mutasse `opts` chegaria tarde demais; o pin sticky (efeito a
  partir da próxima mensagem) evita reconstruir a resolução de rota dentro
  do próprio comando, com o trade-off de não ter a forma "só esta
  mensagem".
- **Corrigir o spawner interno do `SubagentManager`** (só herda `Model` do
  agente-alvo, não workspace/tools/sessão) para preservar isolamento
  completo: não corrigido nesta rodada. A tool `delegate` e o spawn via
  `AgentLoopSpawner.SpawnSubTurn` já preservam workspace/tools/sessão do
  agente-alvo corretamente — são o mecanismo recomendado para cruzar
  agentes (ex.: `sensores` delegando para `estudos`). Ver "Limitações
  conhecidas" abaixo.

## Consequências

- Trocar de agente via `/agent` zera o histórico visível daquela conversa
  para o usuário: cada `AgentInstance` tem `Sessions` próprio, por design —
  é o mesmo isolamento que torna `sensores` e `estudos` seguros um do
  outro. Decisão de produto consciente do André, não surpresa de
  implementação.
- `cfg.Sleep` continua global, não por-agente — irrelevante se cada
  persona rodar em instâncias de processo separadas (o caso mais provável:
  o Raspberry Pi da propriedade rural é um processo Kuromatsu inteiramente
  separado da instância "estudos"), mas seria uma limitação real se as duas
  personas precisassem rodar no mesmo processo com políticas de sono
  diferentes.
- N agentes com heartbeat habilitado disparam turnos de heartbeat
  escalonados (`heartbeatStartStagger`, 2s entre agentes) em vez de todos
  ~1s após o boot — evita uma fila real de turnos de modelo local
  concorrendo pelo mesmo lock do `runstate.Engine` em hardware modesto.

## Limitações conhecidas (não corrigidas nesta rodada)

- **Spawner interno do `SubagentManager`** (`pkg/agent/agent_init.go`, o
  `SetSpawner` usado pelo mecanismo assíncrono do próprio manager) só herda
  o `Model` do agente-alvo, não seu workspace/tools/sessão. Usar a tool
  `delegate` (ou o spawn via `AgentLoopSpawner.SpawnSubTurn`) para cruzar
  agentes, não esse caminho interno.
- **PSI pode estar ausente em alguns kernels ARM/Raspberry Pi**: o
  `memguard` (ADR-017) se desliga sozinho com aviso quando `/proc/pressure`
  não existe — o que é seguro (não crasha), mas remove a rede de segurança
  contra OOM que o resto da arquitetura pressupõe. Verificar no hardware
  real (`cat /proc/pressure/memory`) antes de confiar nisso num Raspberry
  Pi de produção; se ausente, usar `MemoryMax`/swap mais conservadores
  nesse device.
- **Blindagem de "compact prompt"/`MemoryMode` para providers pagos é
  convenção de configuração, não garantia estrutural**: nada impede uma
  janela de foco "cheia" (`system_prompt.mode: full`, `memory: default`)
  rotear para um candidato não-local. Se necessário depois, a opção mais
  barata é um warn-log quando um candidato não-local roda com
  `MemoryMode != off` acima de um limiar de tokens — não implementado
  agora, por falta de um caso de uso real que o exija.
