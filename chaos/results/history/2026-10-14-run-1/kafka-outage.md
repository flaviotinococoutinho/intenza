# Broker fora por 30 segundos

**Veredito: hipótese sustentada** · risco `broker-outage` · 2026-10-14T08:07:18Z → 2026-10-14T08:09:46Z

> Com o broker inalcançável por 30 s, a API continua aceitando escrita sem 5xx, nenhum evento se perde e o backlog do outbox drena em até 60 s depois da volta.

| causa | mecanismo | efeito esperado |
|---|---|---|
| O proxy do Kafka é desligado no Toxiproxy. | O relay não publica e o outbox acumula; a escrita grava estado e evento no Mongo, sem depender do broker. | Projeções e saga atrasam, a escrita segue, e depois da volta o backlog drena. |

| fase | o quê | ok | valor | detalhe |
|---|---|---|---:|---|
| steady | api viva | sim | 204 | status 204 |
| steady | outbox em dia | sim | 0 | ≤ 10 |
| method | espera 15s | sim |  |  |
| method | toxiproxy kafka enabled=false | sim |  |  |
| method | espera 25s | sim |  |  |
| method | probe backlog cresceu durante a queda | sim | 54 | ≥ 1 |
| method | espera 5s | sim |  |  |
| method | toxiproxy kafka enabled=true | sim |  |  |
| rollback | toxiproxy reset | sim |  |  |
| verify | nenhum evento esquecido no outbox | sim | 0 | ≤ 0 |
| verify | backlog drenado | sim | 0 | ≤ 10 |
| verify | publicação sem 5xx | sim | 0 | ≤ 0.01 |
| verify | propostas sem 5xx | sim | 0 | ≤ 0.01 |

### Carga, do lado de quem usa

| operação | n | p50 | p95 | p99 | 5xx | 4xx | erro de rede |
|---|---:|---:|---:|---:|---:|---:|---:|
| publish | 33 | 0.005 | 0.006 | 0.007 | 0 | 0 | 0 |
| propose | 135 | 0.005 | 0.006 | 0.007 | 0 | 4 | 0 |
| rank | 33 | 0.003 | 0.004 | 0.005 | 0 | 0 | 0 |
| award | 33 | 0.007 | 0.008 | 0.010 | 0 | 0 | 0 |
| all | 336 | 0.005 | 0.007 | 0.008 | 0 | 4 | 0 |
