# Evento que nenhum consumidor consegue aplicar

**Veredito: hipótese refutada** · risco `poison-message` · 2026-10-14T08:11:14Z → 2026-10-14T08:14:26Z

> Um evento de acordo impossível de aplicar vai para a fila de mortos depois das tentativas e o consumo segue, com acordos novos ganhando escrow.

| causa | mecanismo | efeito esperado |
|---|---|---|
| Um evento de acordo formado aponta para um acordo que não existe. | O consumidor tenta três vezes com jitter, estaciona o registro na DLQ e confirma o offset. | Um registro na DLQ, partição livre. |

| fase | o quê | ok | valor | detalhe |
|---|---|---|---:|---|
| steady | worker vivo | sim | 204 | status 204 |
| method | falha psp map[auto_pay_after:2s] | sim |  |  |
| method | espera 5s | sim |  |  |
| method | insere em outbox | sim |  |  |
| method | espera 40s | sim |  |  |
| rollback | falha psp map[auto_pay_after:0s] | sim |  |  |
| verify | o evento foi para a DLQ | **não** | 0 | ≥ 1 |
| verify | acordos novos seguiram liberando | sim | 11 | ≥ 1 |

### Carga, do lado de quem usa

| operação | n | p50 | p95 | p99 | 5xx | 4xx | erro de rede |
|---|---:|---:|---:|---:|---:|---:|---:|
| publish | 29 | 0.005 | 0.006 | 0.007 | 0 | 0 | 0 |
| propose | 107 | 0.005 | 0.006 | 0.007 | 0 | 1 | 0 |
| rank | 29 | 0.003 | 0.005 | 0.006 | 0 | 0 | 0 |
| award | 28 | 0.007 | 0.009 | 0.021 | 0 | 0 | 0 |
| ship | 11 | 0.003 | 0.006 | 0.006 | 0 | 0 | 0 |
| deliver | 11 | 0.004 | 0.006 | 0.006 | 0 | 0 | 0 |
| accept | 11 | 0.004 | 0.004 | 0.004 | 0 | 0 | 0 |
| released | 11 | 0.000 | 0.000 | 0.000 | 0 | 0 | 0 |
| all | 1934 | 0.001 | 0.005 | 0.007 | 0 | 1 | 0 |
