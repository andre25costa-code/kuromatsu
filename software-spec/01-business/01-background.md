---
id: S01
title: Contexto do projeto
status: confirmed
version: 2
owner: André
last_updated: 2026-09-11
depends_on: []
---

# S01 — Contexto do projeto

## Origem

O **Kuromatsu** é um fork do [PicoClaw](https://github.com/sipeed/picoclaw) — agente de IA
em Go, ultraleve (~10 MB de RSS), no estilo OpenClaw/Clawdbot. O nome vem do
*kuromatsu* (pinheiro-negro japonês), espécie clássica de bonsai que simboliza
**resiliência em condições extremas** e longevidade — a tese do projeto em uma palavra.

## Problema

Agentes de IA úteis hoje dependem de chaves de API pagas e de conexão constante com
provedores externos. Para um assistente pessoal de segundo plano — que planeja o dia,
executa crons e heartbeats, resume RSS e PDFs, apoia estudos — isso significa custo
recorrente, dependência de rede e envio de dados pessoais a terceiros.

Ao mesmo tempo, os modelos **Bonsai** (PrismML) mudam a economia do problema: são
treinados já quantizados em 1 bit (menos alucinação, boa estabilidade) e o de 1.7B
ocupa ~237 MB em disco. Numa VPS comparável à meta, o modelo sozinho rendeu ~4–6
tokens/s — lento para chat em tempo real, suficiente para tarefas assíncronas.

## Visão

Um agente pessoal que:

1. **Roda sem nenhuma chave de API** — a inferência do Bonsai-1.7B-Q1_0 é embutida no
   próprio binário Go (fork prism do llama.cpp, linkado estático via cgo), sem
   llama-server, sem expor modelo na rede, sem subprocessos.
2. **Cabe em uma máquina Oracle x86_64 (AMD EPYC 7551, Zen1) com 1 GB de RAM**, operando
   24/7 em Docker.
3. **Usa modelos externos quando disponíveis** — chaves em `config.json`/`.security.yml`
   colocam APIs na frente da cadeia; o modelo local é o fallback nativo permanente.
4. **Responde no seu tempo**: mensagens programadas, lembretes, crons e heartbeats usam
   atalhos determinísticos/heurísticos; o LLM entra onde linguagem é necessária.
5. **Se aperfeiçoa dormindo**: um "modo dormir" opcional consolida memória e aprendizados
   em rodadas noturnas/semanais (ver FR-010).
