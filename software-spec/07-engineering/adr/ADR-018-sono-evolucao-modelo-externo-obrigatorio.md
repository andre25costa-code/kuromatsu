---
id: ADR-018
title: Modo dormir e evolução só operam com modelo externo configurado (supera cláusula da ADR-006)
status: accepted
version: 1
owner: André
last_updated: 2026-09-12
depends_on: [ADR-006, ADR-016]
---

# ADR-018 — Sono e evolução só com modelo externo

## Contexto
A ADR-006 decidiu que "qualquer provider serve, inclusive o próprio `bonsai-local`"
para o "inconsciente" do modo dormir. No desenho desta sessão, para o alvo `demetrius`
(e2-micro, modelo de CPU *bursting*: 0,25 vCPU sustentado + créditos de burst — ver
ADR-013, S06 R7), o André identificou três motivos concretos para endurecer essa regra:

1. **Orçamento de CPU**: uma rodada de "sonhar" de madrugada (ou um loop de uso de
   tools do próprio Bonsai) pode esgotar os créditos de burst e jogar o dia seguinte
   inteiro no piso estrangulado.
2. **Qualidade/integridade**: o Bonsai-1.7B-`Q1_0` nunca deveria reescrever
   `MEMORY.md` diretamente — risco real de corromper a própria memória de longo prazo
   com alucinações sintáticas (o mesmo risco já registrado em S06 R2 para tool-calls).
3. **RAM**: se a madrugada ficar I/O-bound (só coordenando chamadas de rede a um
   provider externo) em vez de CPU-bound (rodando o Bonsai local), a VM sobra com
   ~700 MB livres — estabilidade máxima para o backup noturno (S33) e
   `unattended-upgrades`, sem risco de OOM.

## Decisão
1. **`sleep.unconscious_model` passa a ser obrigatório e não pode resolver para
   `provider: native`** — validado em `LoadConfig` (`ValidateSleep`).
2. **Sem um modelo externo válido configurado, o sono fica desligado** mesmo com
   `sleep.enabled: true`: o Kuromatsu sobe e conversa normalmente com o Bonsai nativo,
   só a consolidação noturna não roda — log claro: *"modo dormir requer um modelo
   externo em `sleep.unconscious_model`"*. Não é um erro fatal de boot.
3. **Qualquer provider com chave de API serve** (Ollama Cloud, OpenAI, Anthropic,
   etc.) — a trava é só "não pode ser o `bonsai-local` nativo", não uma lista fechada
   de providers permitidos. Exemplo de entrada Ollama Cloud (OpenAI-compatível) na
   `model_list`: `{model_name: "sonho-cloud", provider: "openai", api_base:
   "https://ollama.com/v1", model: "<modelo>"}`, chave em `.security.yml`,
   `sleep.unconscious_model: "sonho-cloud"` — consumindo créditos free.
4. **`max_tokens_budget`** (FR-010, já existente) continua limitando o consumo por
   rodada, agora com o papel adicional de não estourar créditos free do provider
   externo escolhido.
5. **Mesma regra para a evolução** (`pkg/evolution`, que também chama LLM): se
   ligada, exige um modelo externo configurado ou não roda. O builder de config
   verifica se `Evolution` já expõe um campo de modelo; se não, adiciona
   `evolution.model` com a mesma validação de `ValidateSleep`.

Esta ADR substitui **especificamente a cláusula 2 da Decisão da ADR-006**
("`sleep.unconscious_model`… qualquer provider serve, inclusive o próprio
`bonsai-local`. Vazio ⇒ chain padrão do agente"). As demais decisões da ADR-006
(agendador espelhando `evolution_bridge`, pipeline coleta→triagem→aplicação,
guardrails BR-001/BR-005/BR-006) permanecem em vigor e são estendidas — não
contradizidas — por esta ADR; ver nota de supersessão parcial na própria ADR-006.

## Alternativas
- **Manter "qualquer provider serve" e só recomendar um externo na documentação**:
  rejeitada pelo André — sem uma trava técnica, o padrão seguro não é o padrão
  real observado; o comportamento por omissão continuaria sendo "sonha com os
  próprios pesos", exatamente o cenário que se quer evitar.
- **Desligar o modo dormir inteiramente enquanto não houver um modelo externo padrão
  no projeto**: rejeitada — o `dry_run` e o relatório de sono continuam úteis mesmo
  sem consolidação real aplicada; a trava é especificamente sobre **quem escreve** em
  `MEMORY.md`, não sobre a existência da feature.

## Consequências
- O Bonsai-1.7B nunca mais reescreve `MEMORY.md` por conta própria.
- A madrugada fica I/O-bound quando o sono está ativo (créditos de CPU do e2-micro
  regeneram para o dia seguinte; ~700 MB livres para backup/`unattended-upgrades` sem
  risco de OOM).
- Usuários sem nenhuma chave de API externa (nem free tier) perdem a consolidação
  noturna — aceitável: o agente continua 100% funcional para conversar sem isso; é uma
  feature opcional, e BR-005 já define o sono como desligado por padrão.
- FR-010 ganha uma nova AC refletindo essa trava (ver S11); ADR-006 é marcada
  `superseded_by: ADR-018` **apenas para a cláusula 2** de sua Decisão.
