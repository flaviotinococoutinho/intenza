# Webhooks do agente perdidos e depois duplicados

**Veredito: hipótese refutada** · risco `webhook-loss` · 2026-10-14T08:18:03Z → 2026-10-14T08:23:15Z

> Com todos os webhooks perdidos por 60 s e depois duplicados, a reconciliação leva todo acordo ao estado do agente em até 2 minutos e nenhum termina preso esperando.

| causa | mecanismo | efeito esperado |
|---|---|---|
| O simulador descarta e depois duplica todo webhook. | Sem confirmação, os acordos ficam esperando; a reconciliação pergunta ao agente a cada 10 s e aplica o que ele diz; duplicatas caem na idempotência da máquina de estados. | Liquidação atrasa enquanto os webhooks somem e converge pela reconciliação. |

| fase | o quê | ok | valor | detalhe |
|---|---|---|---:|---|
| steady | api viva | sim | 204 | status 204 |
| method | falha psp map[auto_pay_after:2s webhook_drop:1] | sim |  |  |
| method | espera 1m0s | sim |  |  |
| method | falha psp map[webhook_drop:0 webhook_dup:1] | sim |  |  |
| method | espera 20s | sim |  |  |
| rollback | falha psp map[auto_pay_after:0s webhook_drop:0 webhook_dup:0] | sim |  |  |
| verify | nenhum acordo preso esperando o agente | **não** | 13 | ≤ 0 |
| verify | liberações aconteceram | sim | 18 | ≥ 1 |

### Carga, do lado de quem usa

| operação | n | p50 | p95 | p99 | 5xx | 4xx | erro de rede |
|---|---:|---:|---:|---:|---:|---:|---:|
| publish | 31 | 0.005 | 0.006 | 0.006 | 0 | 0 | 0 |
| propose | 113 | 0.005 | 0.007 | 0.009 | 0 | 2 | 0 |
| rank | 31 | 0.003 | 0.004 | 0.004 | 0 | 0 | 0 |
| award | 31 | 0.007 | 0.009 | 0.009 | 0 | 0 | 0 |
| ship | 18 | 0.004 | 0.005 | 0.005 | 0 | 0 | 0 |
| deliver | 18 | 0.004 | 0.005 | 0.005 | 0 | 0 | 0 |
| accept | 18 | 0.004 | 0.005 | 0.005 | 0 | 0 | 0 |
| released | 18 | 0.000 | 0.000 | 0.000 | 0 | 0 | 0 |
| all | 1919 | 0.001 | 0.005 | 0.007 | 0 | 2 | 0 |
