# Relógio da API adiantado em uma hora

**Veredito: hipótese refutada** · risco `clock-skew` · 2026-10-14T08:06:03Z → 2026-10-14T08:06:03Z

> Com o relógio da API adiantado em uma hora, a intenção aparece fechada para quem lê, mas o comprador continua sem ver preço.

| causa | mecanismo | efeito esperado |
|---|---|---|
| O relógio local do nó da API é deslocado. | A leitura usa o relógio local para o estado efetivo; a abertura dos envelopes só olha o estado gravado, que só muda sob o relógio do banco. | Exibição adiantada, preço selado. |

| fase | o quê | ok | valor | detalhe |
|---|---|---|---:|---|
| steady | api viva | sim | 204 | status 204 |
| method | falha api map[clock_skew:PT1H] | sim |  |  |
| method | probe preço continua selado | **não** |  | o relógio não estava adiantado (efetivo "expired"): sonda inválida |
| rollback | falha api map[clock_skew:PT0S] | sim |  |  |
