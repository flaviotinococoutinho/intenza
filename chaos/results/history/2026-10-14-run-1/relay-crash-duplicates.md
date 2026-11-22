# Relay cai entre publicar e marcar

**Veredito: hipótese refutada** · risco `duplicate-processing` · 2026-10-14T08:25:05Z → 2026-10-14T08:26:48Z

> Quando o relay cai depois de publicar e antes de marcar o outbox, os eventos saem de novo e mesmo assim nenhum vendedor recebe a mesma notificação duas vezes.

| causa | mecanismo | efeito esperado |
|---|---|---|
| Falha injetada que encerra o relay logo depois do ack do broker. | Ao voltar, o relay republica o que não foi marcado, com os mesmos ids; o consumidor de fan-out aplica cada evento uma vez pelo inbox. | Duplicatas no Kafka, nenhuma no efeito. |

| fase | o quê | ok | valor | detalhe |
|---|---|---|---:|---|
| steady | relay vivo | sim | 204 | status 204 |
| method | espera 15s | sim |  |  |
| method | falha relay map[crash_after_publish:true] | sim |  |  |
| method | espera 30s | sim |  |  |
| verify | relay voltou | sim | 204 | status 204 |
| verify | notificações em dobro | sim | 0 | ≤ 0 |
| verify | duplicatas chegaram e foram absorvidas | **não** | 0 | ≥ 1 |
| verify | nada preso no outbox | sim | 0 | ≤ 0 |

### Carga, do lado de quem usa

| operação | n | p50 | p95 | p99 | 5xx | 4xx | erro de rede |
|---|---:|---:|---:|---:|---:|---:|---:|
| publish | 41 | 0.005 | 0.005 | 0.006 | 0 | 0 | 0 |
| propose | 120 | 0.005 | 0.006 | 0.007 | 0 | 1 | 0 |
| rank | 41 | 0.003 | 0.004 | 0.004 | 0 | 0 | 0 |
| award | 40 | 0.006 | 0.008 | 0.010 | 0 | 0 | 0 |
| all | 388 | 0.004 | 0.007 | 0.008 | 0 | 1 | 0 |
