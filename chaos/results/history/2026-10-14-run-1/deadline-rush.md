# Corrida de última hora no fechamento

**Veredito: hipótese sustentada** · risco `deadline-herd` · 2026-10-14T08:06:03Z → 2026-10-14T08:07:18Z

> Com lances concentrados nos últimos instantes e duas intenções por segundo, nenhuma proposta entra depois do prazo e o p99 de envio fica abaixo de 500 ms.

| causa | mecanismo | efeito esperado |
|---|---|---|
| Vendedores deixam para o fim, como em todo mercado de proposta selada. | A taxa de chegada explode perto do prazo; a guarda pelo relógio do banco e o conflito de escrita decidem a fronteira. | Pico de carga no instante do fechamento, sem proposta tardia aceita. |

| fase | o quê | ok | valor | detalhe |
|---|---|---|---:|---|
| steady | api viva | sim | 204 | status 204 |
| method | espera 45s | sim |  |  |
| verify | propostas aceitas depois do prazo | sim | 0 | ≤ 0 |
| verify | p99 de proposta abaixo de 500 ms | sim | 0.01664 | ≤ 0.5 |
| verify | propostas sem 5xx | sim | 0 | ≤ 0.01 |

### Carga, do lado de quem usa

| operação | n | p50 | p95 | p99 | 5xx | 4xx | erro de rede |
|---|---:|---:|---:|---:|---:|---:|---:|
| publish | 92 | 0.005 | 0.006 | 0.014 | 0 | 0 | 0 |
| propose | 878 | 0.005 | 0.009 | 0.017 | 0 | 19 | 0 |
| rank | 92 | 0.006 | 0.009 | 0.012 | 0 | 0 | 0 |
| award | 92 | 0.010 | 0.015 | 0.028 | 0 | 0 | 0 |
| all | 1358 | 0.005 | 0.011 | 0.015 | 0 | 19 | 0 |
