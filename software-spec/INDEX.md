# Kuromatsu — Especificação (Spec-Driven Delivery)

Mapa da documentação de especificação. A metodologia segue a skill `software-spec-writing`:
só entra aqui o que foi **confirmado**; o que falta está registrado como estado em
`spec-coverage.yaml`, nunca como capítulo vazio.

## Ordem de leitura sugerida

1. `01-business/01-background.md` — por que o Kuromatsu existe
2. `01-business/06-assumptions.md` — premissas, restrições e questões abertas
3. `02-business-design/10-scope.md` — o que está dentro e fora do fork
4. `02-business-design/11-requirements.md` — **FR/AC: a espinha dorsal** (Gate: sem FR+AC não se escreve código de feature)
5. `03-system-design/13-architecture.md` + `14-modules.md` — arquitetura
6. `03-system-design/18-ai-component.md` — o componente de IA (Bonsai-1.7B-Q1_0)
7. `06-infrastructure/29-nfr.md` — metas quantificadas (RAM, tok/s)
8. `07-engineering/adr/` — decisões de arquitetura (ADR-001..018; ADR-011 `superseded_by: ADR-012`, ADR-006 `superseded_by: ADR-018`)

## Arquivos de controle

| Arquivo | Papel |
|---|---|
| `spec.manifest.yaml` | Índice para agentes: item → arquivo, tags, papéis |
| `spec-coverage.yaml` | Estado dos 40 itens de consideração (confirmed/draft/tbd/n/a/missing) |
| `entregues/E0..E7,E10-*.md` | Auditoria dos entregáveis já fechados: FRs cobertas, commits, medições |
| `BACKLOG.md` | Só o que falta: entregáveis abertos e questões pendentes |

## Regras vigentes

- IDs (`FR-`, `AC-`, `BR-`, `NFR-`, `ADR-`) nunca mudam nem são reutilizados.
- Toda escrita nesta pasta atualiza `spec-coverage.yaml` na mesma tarefa e lista os
  itens afetados via `depends_on`.
- Diagramas sempre em Mermaid.
- Idioma: PT-BR.
