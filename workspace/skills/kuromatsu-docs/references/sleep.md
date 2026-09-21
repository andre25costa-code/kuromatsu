# Modo dormir (consolidação noturna)

Fonte real: `pkg/sleep/`, `pkg/agent/sleep_bridge.go`, `pkg/config/sleep.go`.
ADR-006/008/018.

## O que faz

Numa janela horária configurada, coleta digests de sessão (texto recente +
resumo já existente), manda em lotes pra um modelo **externo** (nunca o
Bonsai nativo — ver "Exige modelo externo" abaixo), e aplica o resultado em
`MEMORY.md` de forma atômica. Escreve um relatório
`state/sleep-report-<data>.md` a cada execução, com ou sem `dry_run`.

## Config (`sleep`)

```json
{
  "sleep": {
    "enabled": false,
    "window": "03:00-05:00",
    "unconscious_model": "sonho-cloud",
    "weekly_deep": false,
    "max_tokens_budget": 20000,
    "dry_run": false
  }
}
```

`enabled: false` (default) — nenhuma goroutine/timer criada, sem custo
nenhum em runtime.

## Exige modelo externo (ADR-018)

`sleep.unconscious_model` (e, se a evolução usa cold path, `evolution.model`
também) **tem que** resolver pra um provider que não seja nativo —
validado em `LoadConfig` (`hasValidExternalModel`, agora delega pra
`common.IsNativeModel`, mesma checagem usada no catálogo de providers —
única fonte, sem risco de um alias novo escapar de um dos dois lugares).
Sem modelo externo válido, o sono fica desligado com um log claro em vez de
deixar o Bonsai "sonhar com os próprios pesos" — motivo: orçamento de CPU
de uma VM pequena, e o Bonsai 1-bit nunca reescreve `MEMORY.md` sozinho.

## Cursor por sessão (corrigido no audit de 2026-09-16, MEM-01)

Cada sessão tem um cursor **baseado em hash de conteúdo**
(`state/sleep-cursors.json`, persistido — sobrevive a restart), avançado
só depois que o lote correspondente foi de fato processado e a memória
salva (`agentSessionSource.Acknowledge`, chamado por `RunColdPathOnce`
depois de escrever `MEMORY.md`). Antes disso o cursor era uma contagem de
mensagens em memória só, avançada durante a **coleta** — ou seja, antes da
consolidação de fato acontecer; se a chamada ao modelo falhasse no meio,
essas sessões ficavam marcadas como "já vistas" e nunca eram tentadas de
novo. Não confiar em nenhum outro lugar do código que pareça reintroduzir
esse padrão (avançar cursor/marcar "visto" antes de confirmar sucesso).

## Orçamento de tokens (corrigido no audit, MEM-02)

`pkg/sleep/budget.go`: cada chamada reserva conservadoramente
`len(system)+len(user)+512` bytes de entrada e manda `max_tokens` real ao
provider (`OutputTokenLimit`); se o lote não cabe, ele encolhe (remove
digests do fim) até caber, em vez de descartar um digest só porque o vizinho
inflou o lote. Se o provider não reporta uso de tokens (`TotalTokens <= 0`),
o Kuromatsu estima pelo tamanho do texto em vez de contar zero — sem essa
estimativa, o orçamento nunca detectava esgotamento com um provider desses.

## Deadline propagado até a chamada (corrigido no audit, MEM-03)

O fim da janela agendada agora é um `context.WithDeadline` que chega até a
chamada real ao modelo (`runOnceContext`/`runTriage`), não só um valor
comparado depois. Cancelamento no meio de uma chamada impede persistir
memória parcial ou confirmar o cursor daquele lote — refaz do zero na
próxima janela (mesma semântica de retry-do-zero que o resto do desenho já
tinha, S17).

## O que o modo dormir NÃO garante

- Não é uma transação única entre `MEMORY.md` e o cursor: falha entre
  persistir a memória e confirmar o cursor pode reprocessar o mesmo lote —
  semântica "ao menos uma vez", não exatamente-uma-vez.
- O teto de `max_tokens` mandado ao provider depende do provider de fato
  respeitar esse parâmetro — não é garantia de faturamento contra um
  serviço que o ignore.

## Sono por agente (`agents.list[].sleep`)

Cada agente pode sobrescrever `enabled`/`window`/`unconscious_model`/
`weekly_deep`/`max_tokens_budget`/`dry_run` individualmente (mesmo padrão
de `agents.list[].heartbeat` — sobrescreve campo a campo, herda o resto do
bloco `sleep` global). `Config.EffectiveSleep(agentID)` resolve o efetivo;
`pkg/agent/sleep_bridge.go` agrupa os agentes pelo `SleepConfig` efetivo e
cria um agendamento (`sleepBridge`) independente por grupo — dois agentes
com janela/modelo diferentes no mesmo processo rodam sono em horários
diferentes, sem interferir um no outro; um agente com
`sleep: {enabled: false}` próprio fica de fora mesmo com o sono global
ligado para os demais. `ValidateSleep` aplica o mesmo gate do ADR-018
(modelo externo, não-nativo) a cada override por agente, não só ao bloco
global.

```json
{
  "sleep": { "enabled": true, "unconscious_model": "sonho-cloud" },
  "agents": {
    "list": [
      { "id": "sensores", "sleep": { "enabled": false } },
      { "id": "estudos", "sleep": { "window": "01:00-02:00" } }
    ]
  }
}
```

Sem `agents.list` (o caso comum), ou sem nenhum agente sobrescrevendo
`sleep`, o comportamento é idêntico a um único agendamento global — nada
muda pra quem não usa multi-agente.
