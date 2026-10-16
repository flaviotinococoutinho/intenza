# Banco congelado por 15 segundos

**Veredito: hipótese refutada** · risco `database-outage` · 2026-10-14T08:09:46Z → 2026-10-14T08:11:14Z

> Com o banco congelado por 15 s, a API responde em menos de 2,5 s mesmo quando falha (503 em vez de pendurar) e volta sozinha quando o banco volta.

| causa | mecanismo | efeito esperado |
|---|---|---|
| O container do MongoDB é pausado. | Toda operação tem prazo no cliente (CSOT de 2 s); estourou, a API devolve 503 com Retry-After. | Erros rápidos durante a pausa, recuperação sem intervenção. |

| fase | o quê | ok | valor | detalhe |
|---|---|---|---:|---|
| steady | api pronta | sim | 200 | status 200 |
| method | espera 15s | sim |  |  |
| method | docker pause mongo | sim |  |  |
| method | espera 15s | sim |  |  |
| method | docker unpause mongo | sim |  |  |
| rollback | docker unpause mongo | **não** |  | docker unpause intenza-mongo-1: 500 {"message":"Container 0e7254dd1e205a40798261507b6e671e96f86cfbe91d96f23cd3312bf415406e is not paused"}
 |
| verify | p99 de publicação abaixo de 2 | **não** | 4.003 | ≤ 2.5 |
| verify | p99 de leitura abaixo de 2 | sim | 2.002 | ≤ 2.5 |
| verify | api pronta de novo | sim | 200 | status 200 |

### Carga, do lado de quem usa

| operação | n | p50 | p95 | p99 | 5xx | 4xx | erro de rede |
|---|---:|---:|---:|---:|---:|---:|---:|
| publish | 18 | 0.005 | 4.003 | 4.003 | 8 | 0 | 0 |
| propose | 34 | 0.007 | 4.004 | 4.004 | 17 | 0 | 0 |
| rank | 10 | 0.002 | 0.004 | 0.004 | 0 | 0 | 0 |
| award | 7 | 0.006 | 0.007 | 0.007 | 0 | 0 | 0 |
| all | 116 | 0.005 | 2.002 | 4.004 | 33 | 0 | 0 |
