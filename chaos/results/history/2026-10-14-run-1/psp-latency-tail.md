# Agente de custódia lento e instável

**Veredito: hipótese sustentada** · risco `psp-latency-tail` · 2026-10-14T08:14:26Z → 2026-10-14T08:18:03Z

> Com o agente de custódia em cauda lognormal (mediana 800 ms, sigma 1) e 30% de 503, o p99 de publicação e de proposta fica abaixo de 1 s e os acordos ainda chegam à liberação.

| causa | mecanismo | efeito esperado |
|---|---|---|
| Latência e erros injetados no simulador do agente. | A saga é assíncrona; cada chamada tem prazo, disjuntor por taxa de falha e retentativa com jitter, então a lentidão fica no worker. | A liquidação atrasa e o disjuntor pode abrir; quem publica e propõe não sente. |

| fase | o quê | ok | valor | detalhe |
|---|---|---|---:|---|
| steady | api viva | sim | 204 | status 204 |
| steady | agente vivo | sim | 204 | status 204 |
| method | falha psp map[auto_pay_after:2s] | sim |  |  |
| method | espera 10s | sim |  |  |
| method | falha psp map[error_rate:0.3 latency_median:800ms latency_sigma:1] | sim |  |  |
| method | espera 1m0s | sim |  |  |
| method | falha psp map[error_rate:0 latency_median:0s latency_sigma:0] | sim |  |  |
| rollback | falha psp map[auto_pay_after:0s error_rate:0 latency_median:0s latency_sigma:0] | sim |  |  |
| verify | p99 de publicação abaixo de 1 s | sim | 0.006418 | ≤ 1 |
| verify | p99 de proposta abaixo de 1 s | sim | 0.00911 | ≤ 1 |
| verify | o worker sentiu a falha | sim | 363.2 | ≥ 1 |
| verify | acordos liberados apesar da cauda | sim | 9 | ≥ 1 |

### Carga, do lado de quem usa

| operação | n | p50 | p95 | p99 | 5xx | 4xx | erro de rede |
|---|---:|---:|---:|---:|---:|---:|---:|
| publish | 41 | 0.005 | 0.006 | 0.006 | 0 | 0 | 0 |
| propose | 154 | 0.005 | 0.007 | 0.009 | 0 | 1 | 0 |
| rank | 41 | 0.003 | 0.004 | 0.007 | 0 | 0 | 0 |
| award | 41 | 0.007 | 0.008 | 0.014 | 0 | 0 | 0 |
| ship | 9 | 0.003 | 0.006 | 0.006 | 0 | 0 | 0 |
| deliver | 9 | 0.004 | 0.005 | 0.005 | 0 | 0 | 0 |
| accept | 9 | 0.004 | 0.004 | 0.004 | 0 | 0 | 0 |
| released | 9 | 0.000 | 0.000 | 0.000 | 0 | 0 | 0 |
| all | 3571 | 0.001 | 0.005 | 0.007 | 0 | 1 | 0 |
