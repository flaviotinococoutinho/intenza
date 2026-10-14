# Redis fora por 40 segundos

**Veredito: hipótese refutada** · risco `redis-outage` · 2026-10-14T08:23:15Z → 2026-10-14T08:25:05Z

> Sem Redis por 40 s, a API segue sem 5xx, a idempotência continua valendo e os fechamentos acontecem pela varredura com p95 de atraso abaixo de 30 s.

| causa | mecanismo | efeito esperado |
|---|---|---|
| O proxy do Redis é desligado no Toxiproxy. | O limite distribuído cai para o limite local de cada nó e o índice de prazos some; a varredura no Mongo é a fonte da verdade. | Limites ficam por nó e os fechamentos atrasam alguns segundos, mas acontecem. |

| fase | o quê | ok | valor | detalhe |
|---|---|---|---:|---|
| steady | api viva | sim | 204 | status 204 |
| method | espera 10s | sim |  |  |
| method | toxiproxy redis enabled=false | sim |  |  |
| method | espera 10s | sim |  |  |
| method | probe idempotência sem redis | sim | 1 | ≥ 1 |
| method | espera 30s | sim |  |  |
| method | toxiproxy redis enabled=true | sim |  |  |
| rollback | toxiproxy reset | sim |  |  |
| verify | atraso p95 dos fechamentos abaixo de 30 s | **não** | 49.31 | ≤ 30 |
| verify | a varredura assumiu fechamentos | sim | 32.68 | ≥ 1 |
| verify | publicação sem 5xx | sim | 0 | ≤ 0.01 |
| verify | propostas sem 5xx | sim | 0 | ≤ 0.01 |

### Carga, do lado de quem usa

| operação | n | p50 | p95 | p99 | 5xx | 4xx | erro de rede |
|---|---:|---:|---:|---:|---:|---:|---:|
| publish | 45 | 0.005 | 0.006 | 0.008 | 0 | 0 | 0 |
| propose | 160 | 0.005 | 0.008 | 0.010 | 0 | 1 | 0 |
| rank | 45 | 0.003 | 0.004 | 0.005 | 0 | 0 | 0 |
| award | 45 | 0.007 | 0.011 | 0.011 | 0 | 0 | 0 |
| all | 510 | 0.004 | 0.007 | 0.010 | 0 | 1 | 0 |
