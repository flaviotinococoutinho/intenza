<!-- Gerado por `go run ./cmd/intenza-kb render`. Não edite à mão: edite knowledge/*.yaml. -->

# Base de conhecimento

Três perguntas organizam o Intenza: **o que existe** (ontologia), **como a gente sabe** (epistemologia) e **o que pode dar errado** (riscos). Cada resposta é dado versionado em `knowledge/` e um verificador confere contra o código, os testes e os experimentos de caos.

## Estado epistêmico

A priori vale para toda entrada no escopo (conhecimento determinístico). A posteriori sobreviveu a uma tentativa de refutação no sistema rodando (conhecimento estocástico). Testemunho é emprestado de uma autoridade. Conjectura é dívida.

| base | modo | força | alegações |
|---|---|---:|---:|
| por construção | a priori | 7 | 0 |
| prova exaustiva | a priori | 6 | 0 |
| teste de propriedade | a priori | 5 | 0 |
| teste de exemplo | a priori | 4 | 0 |
| medido | a posteriori | 3 | 0 |
| testemunho | testemunhal | 2 | 0 |
| conjectura | sem justificativa | 1 | 31 |

Dívida epistêmica: **31 de 31** alegações ainda são conjecturas.

## Riscos

Cada risco é uma cadeia causa → mecanismo → efeito. A solução é determinística (remove o modo de falha e se justifica a priori), estocástica (reduz probabilidade ou impacto e só se justifica medindo) ou híbrida (piso determinístico e teto medido).

### Matriz incerteza × solução

| | determinística | estocástica | híbrida |
|---|---|---|---|
| **aleatória** | clock-skew, double-award, double-settlement, duplicate-intent, duplicate-processing, invalid-identifier, late-bid, lost-event | cache-stampede, database-outage, deadline-herd, psp-latency-tail | broker-outage, redis-outage, webhook-loss |
| **epistêmica** | autoparts-regulatory, biased-ranking, bid-leak, forbidden-category, funds-custody, he-said-she-said, retaliation | cold-start-seller | poison-message |

Mitigados com a evidência certa para a natureza da solução: **0 de 24**.

| risco | S×P | causa → mecanismo → efeito | solução | status | mitigado por | experimentos |
|---|---:|---|---|---|---|---|
| **he-said-she-said**<br>Disputa sem base objetiva | 16 | Descrição livre e prova informal. → Ninguém consegue dizer o que foi prometido. → Resolução arbitrária, custo de suporte e reputação injusta. | determinística | exposto | terms-snapshot-tamper-evident, dispute-is-field-conference |  |
| **duplicate-processing**<br>Evento aplicado duas vezes | 16 | Entrega pelo menos uma vez (retentativa, rebalanceamento, relay que cai antes do checkpoint). → O consumidor reaplica o efeito do mesmo evento. → Reputação contada em dobro, notificação repetida, comando duplicado. | determinística | exposto | consumers-effectively-once |  |
| **webhook-loss**<br>Webhook do agente de custódia perdido, duplicado ou fora de ordem | 16 | Rede, retentativa do agente ou indisponibilidade da API. → A plataforma não fica sabendo que o dinheiro caiu ou foi liberado. → Acordo preso, vendedor sem enviar, comprador sem resposta. | híbrida | exposto | webhook-handling-idempotent, reconciliation-converges |  |
| **psp-latency-tail**<br>Cauda de latência do agente de custódia | 16 | Agente externo com latência lognormal e picos. → Chamadas presas seguram conexões e goroutines e a fila cresce. → Latência da API sobe para todo mundo, não só para quem paga. | estocástica | exposto | psp-calls-bounded |  |
| **bid-leak**<br>Vazamento de preço selado | 15 | Caminho não previsto expõe o preço (log, evento, dump, consulta ampla, revelação antecipada). → Concorrente ou comprador vê preços antes do fechamento e passa a ancorar ou combinar. → A proposta selada vira leilão aberto disfarçado e os melhores vendedores saem. | determinística | exposto | sealed-price-encrypted-at-rest, reveal-gated-by-persisted-close, events-carry-no-price |  |
| **double-settlement**<br>Liberação ou estorno disparado duas vezes, ou os dois | 15 | Evento redelivery, webhook duplicado ou retentativa após timeout. → O comando ao agente de custódia é reenviado sem chave ou o estado não é terminal. → Dinheiro liberado e estornado, ou liberado duas vezes. | determinística | exposto | escrow-command-idempotent, settlement-terminal-exclusive, webhook-handling-idempotent |  |
| **late-bid**<br>Proposta aceita depois do prazo | 12 | Envio e fechamento correm no mesmo instante. → A checagem de prazo e a gravação acontecem em momentos diferentes. → Vendedor que viu o fechamento chegando entra depois e quebra a isonomia. | determinística | exposto | no-bid-after-close |  |
| **invalid-identifier**<br>Identificador digitado errado vira prova falsa | 12 | Erro de digitação em GTIN, VIN, ISBN ou documento. → O campo probatório guarda um identificador que não existe. → Contrato aponta para outro item ou para ninguém. | determinística | exposto | identifiers-check-digit-validated |  |
| **lost-event**<br>Evento perdido entre banco e broker | 12 | Escrita dupla em banco e broker sem transação comum. → O processo cai depois de gravar o estado e antes de publicar. → Projeções e sagas nunca ficam sabendo da mudança. | determinística | exposto | outbox-atomic-with-state |  |
| **duplicate-intent**<br>Retentativa do cliente cria intenção duplicada | 12 | Timeout no cliente depois que o servidor já gravou. → O mesmo pedido chega duas vezes. → Duas intenções iguais e vendedores respondendo em dobro. | determinística | exposto | intent-creation-idempotent |  |
| **broker-outage**<br>Broker indisponível | 12 | Broker cai, pausa ou perde a rede. → O relay não publica e o outbox acumula. → Projeções e sagas atrasam, mas a escrita continua aceitando. | híbrida | exposto | outbox-atomic-with-state, backlog-drains |  |
| **redis-outage**<br>Redis indisponível | 12 | Redis cai, pausa ou fica lento. → Idempotência rápida, limite de taxa, prazos e cache perdem o apoio. → Duplicata, rajada sem limite ou fechamento atrasado se não houver degradação planejada. | híbrida | exposto | intent-creation-idempotent, deadlines-survive-redis-loss |  |
| **deadline-herd**<br>Rajada de última hora no fechamento | 12 | Vendedores deixam para o fim e muitos prazos vencem juntos. → A taxa de chegada cresce perto do prazo e os fechamentos se acumulam. → Latência sobe exatamente quando importa e propostas perdem o prazo. | estocástica | exposto | rate-limit-protects-p99 |  |
| **cold-start-seller**<br>Vendedor novo nunca vence | 12 | Sem histórico, a reputação é desconhecida. → Ordenação por reputação pontual favorece sempre quem já vende. → Concentração de mercado e oferta estagnada. | estocástica | exposto | reputation-shrinks-small-samples, exploration-exposes-new-sellers |  |
| **double-award**<br>Duas propostas escolhidas para a mesma intenção | 10 | Clique duplo, retentativa do cliente ou duas abas. → Duas seleções leem a intenção aberta e gravam em paralelo. → Dois acordos e dois escrows para um item só. | determinística | exposto | single-award-per-intent |  |
| **forbidden-category**<br>Anúncio em categoria proibida | 10 | Erro de curadoria ou categoria genérica demais. → Intenção é publicada num nó que admite item proibido. → Venda de arma, medicamento ou animal pela plataforma. | determinística | exposto | forbidden-categories-unrepresentable |  |
| **autoparts-regulatory**<br>Peça de desmonte sem rastreabilidade ou item de segurança | 10 | Autopeças concentram a maior densidade regulatória. → Vendedor sem credencial ou peça sem etiqueta entra na árvore. → Receptação, risco à segurança viária e sanção. | determinística | exposto | autoparts-restricted-by-design, identifiers-check-digit-validated |  |
| **retaliation**<br>Avaliação retaliatória | 9 | Uma parte vê a avaliação da outra antes de avaliar. → Nota baixa provoca nota baixa de volta. → Reputação vira negociação e perde sinal. | determinística | exposto | ratings-double-blind |  |
| **clock-skew**<br>Relógio de um nó fora do lugar | 8 | NTP falha ou VM pausa e o relógio local adianta ou atrasa. → Decisões de prazo tomadas com o relógio local discordam entre nós. → Proposta aceita depois do prazo ou envelope aberto antes da hora. | determinística | exposto | reveal-gated-by-persisted-close, effective-status-is-time-function |  |
| **database-outage**<br>Banco indisponível | 8 | Primário pausa, cai ou troca. → Chamadas ficam esperando conexão e acumulam. → A API pendura em vez de falhar rápido. | estocástica | exposto | mongo-outage-fails-fast |  |
| **poison-message**<br>Evento malformado trava o consumidor | 6 | Versão de esquema inesperada ou dado corrompido. → O consumidor falha sempre no mesmo offset. → A partição para e o atraso cresce sem limite. | híbrida | exposto | poison-message-isolated |  |
| **cache-stampede**<br>Estouro de recomputação no cache | 6 | Muitas chaves expiram juntas sob carga. → Todas as réplicas recomputam a mesma coisa ao mesmo tempo. → Pico de carga no banco e latência em degrau. | estocástica | exposto | cache-early-refresh |  |
| **biased-ranking**<br>Ordenação arbitrária ou manipulável | 6 | Critério opaco ou dependente da ordem de chegada. → Duas consultas iguais devolvem ordens diferentes. → Comprador desconfia e vendedor otimiza o horário em vez da oferta. | determinística | exposto | selection-deterministic-explainable |  |
| **funds-custody**<br>Dinheiro transitando por conta própria | 5 | Atalho de produto para simplificar repasse ou estorno. → A plataforma passa a custodiar recursos de terceiros. → Exposição regulatória e risco de crédito sobre o valor dos clientes. | determinística | exposto | platform-never-holds-funds, payment-institution-regulated |  |

## Alegações

| alegação | enunciado | tipo | base | evidência |
|---|---|---|---|---|
| **autoparts-restricted-by-design** | Autopeças só aceitam vendedor com credencial de desmonte e peça com código de rastreabilidade. Sistemas de segurança não existem na árvore. | regulatory | conjectura |  |
| **backlog-drains** | Depois de uma queda do broker, o backlog do outbox drena em até 60 s sem perder evento. | liveness | conjectura |  |
| **cache-early-refresh** | Expiração antecipada probabilística evita estouro de recomputação no cache de reputação. | performance | conjectura |  |
| **consumers-effectively-once** | Cada consumidor aplica um evento no máximo uma vez, com inbox único por consumidor e evento na mesma transação do efeito. | integrity | conjectura |  |
| **deadlines-survive-redis-loss** | Sem Redis, os fechamentos acontecem pela varredura no banco com atraso abaixo de 30 s. | liveness | conjectura |  |
| **dispute-is-field-conference** | Disputa é conferência de campos. Cada reclamação compara esperado contra observado de um atributo probatório do schema congelado. | integrity | conjectura |  |
| **effective-status-is-time-function** | O estado efetivo de uma intenção é função pura do relógio recebido. Timer atrasado não muda o que se lê. | safety | conjectura |  |
| **escrow-command-idempotent** | Comandos ao escrow carregam chave de idempotência. Repetir não duplica efeito. | safety | conjectura |  |
| **events-carry-no-price** | Nenhum evento de proposta publicado carrega preço. | safety | conjectura |  |
| **exploration-exposes-new-sellers** | Na notificação de intenções, amostragem de Thompson dá exposição mínima a vendedores sem histórico. | economic | conjectura |  |
| **forbidden-categories-unrepresentable** | Categorias proibidas (armas, medicamentos, animais, tabaco, álcool) não existem na árvore, e um identificador de categoria só nasce de um nó dela. | safety | conjectura |  |
| **identifiers-check-digit-validated** | Identificadores normativos (GTIN, ISBN, VIN, CPF, CNPJ, RENAVAM) passam pelo dígito verificador antes de virar prova. | integrity | conjectura |  |
| **intent-creation-idempotent** | Repetir a criação de intenção com a mesma Idempotency-Key devolve a mesma intenção, com ou sem Redis. | safety | conjectura |  |
| **mongo-outage-fails-fast** | Com o banco pausado, a API responde 503 em menos de 2 s em vez de pendurar, e volta sozinha. | performance | conjectura |  |
| **no-bid-after-close** | Nenhuma proposta é aceita depois do fechamento, nem em corrida com o fechador. | safety | conjectura |  |
| **outbox-atomic-with-state** | Mudança de estado e evento são gravados na mesma transação. Não existe estado sem evento nem evento sem estado. | integrity | conjectura |  |
| **payment-institution-regulated** | Custódia de recursos de terceiros exige instituição de pagamento autorizada pelo Banco Central. | regulatory | conjectura |  |
| **platform-never-holds-funds** | Nenhum caminho de código leva dinheiro para conta da plataforma. A porta do escrow só abre, libera e estorna. | regulatory | conjectura |  |
| **poison-message-isolated** | Evento malformado vai para a fila de mortos depois de 3 tentativas e não trava a partição. | liveness | conjectura |  |
| **psp-calls-bounded** | Chamadas ao agente de custódia têm prazo, disjuntor e retentativa com jitter. Com cauda lognormal, o p99 da API fica abaixo de 1 s. | performance | conjectura |  |
| **rate-limit-protects-p99** | Na corrida de última hora, o limite GCRA mantém o p99 de envio de proposta abaixo de 500 ms para quem está dentro da cota. | performance | conjectura |  |
| **ratings-double-blind** | As avaliações só aparecem quando as duas partes avaliaram ou o prazo venceu. | economic | conjectura |  |
| **reconciliation-converges** | Com webhooks perdidos, a reconciliação leva todo acordo ao estado do agente de custódia em até 2 minutos. | liveness | conjectura |  |
| **reputation-shrinks-small-samples** | Com poucas avaliações, a reputação encolhe para a média da plataforma. Um 5 isolado não supera um histórico longo de 4,8. | economic | conjectura |  |
| **reveal-gated-by-persisted-close** | O preço só abre depois que a intenção foi fechada no banco, sob o relógio do banco. Relógio local adiantado não antecipa a revelação. | safety | conjectura |  |
| **sealed-price-encrypted-at-rest** | O preço da proposta é gravado cifrado com AES-256-GCM, chave derivada por intenção (HKDF) e dados associados que amarram proposta, vendedor e intenção. | integrity | conjectura |  |
| **selection-deterministic-explainable** | A ordenação das propostas é função pura de critérios e pesos, invariante à ordem de chegada, e uma proposta dominada nunca fica acima de quem a domina. | economic | conjectura |  |
| **settlement-terminal-exclusive** | Liberado, estornado e cancelado são terminais e exclusivos. Um acordo nunca libera e estorna. | safety | conjectura |  |
| **single-award-per-intent** | Uma intenção gera no máximo um acordo. | safety | conjectura |  |
| **terms-snapshot-tamper-evident** | Os termos do acordo são congelados em JSON canônico e o SHA-256 vai junto. Qualquer alteração muda o hash. | integrity | conjectura |  |
| **webhook-handling-idempotent** | Webhook duplicado ou fora de ordem não muda o resultado. A máquina de estados recusa transição inválida e o evento é deduplicado. | safety | conjectura |  |

## Ontologia

Categorias inspiradas na UFO: o *kind* dá identidade, *role* e *phase* são anti-rígidos, o *relator* (contrato, escrow) existe entre partes, a categoria é um *tipo de alta ordem* que classifica itens e define as qualidades que eles carregam.

```mermaid
flowchart LR
  party["Parte<br/><i>kind</i>"]:::kind
  person["Pessoa física<br/><i>subkind</i>"]:::subkind
  company["Pessoa jurídica<br/><i>subkind</i>"]:::subkind
  buyer["Comprador<br/><i>role</i>"]:::role
  seller["Vendedor<br/><i>role</i>"]:::role
  escrow_agent["Agente de custódia<br/><i>role</i>"]:::role
  arbiter["Árbitro<br/><i>role</i>"]:::role
  item["Item<br/><i>kind</i>"]:::kind
  niche["Nicho<br/><i>collective</i>"]:::collective
  category["Categoria<br/><i>type</i>"]:::type
  attribute_schema["Schema de atributos<br/><i>information</i>"]:::information
  attribute["Atributo<br/><i>quality</i>"]:::quality
  intent["Intenção de compra<br/><i>social-object</i>"]:::socialobject
  proposal["Proposta selada<br/><i>commitment</i>"]:::commitment
  agreement["Acordo<br/><i>relator</i>"]:::relator
  terms_snapshot["Termos congelados<br/><i>information</i>"]:::information
  escrow["Escrow<br/><i>relator</i>"]:::relator
  field_claim["Reclamação de campo<br/><i>information</i>"]:::information
  rating["Avaliação<br/><i>social-object</i>"]:::socialobject
  reputation["Reputação<br/><i>quality</i>"]:::quality
  event_record["Registro de evento<br/><i>information</i>"]:::information
  person -->|especializa| party
  company -->|especializa| party
  buyer -->|especializa| party
  seller -->|especializa| party
  escrow_agent -->|especializa| company
  arbiter -->|especializa| party
  category -->|classifica| item
  category -->|parte de| niche
  attribute_schema -->|parte de| category
  attribute -->|inere em| item
  agreement -->|media| buyer
  agreement -->|media| seller
  terms_snapshot -->|parte de| agreement
  escrow -->|media| buyer
  escrow -->|media| seller
  escrow -->|media| escrow_agent
  reputation -->|inere em| party
  classDef kind fill:#1f6f5c,color:#fff
  classDef subkind fill:#2e8b74,color:#fff
  classDef role fill:#d98e04,color:#fff
  classDef relator fill:#7a3e9d,color:#fff
  classDef commitment fill:#b5446e,color:#fff
  classDef socialobject fill:#c25b3f,color:#fff
  classDef type fill:#2b5d9b,color:#fff
  classDef collective fill:#4f7cac,color:#fff
  classDef quality fill:#6b8f23,color:#fff
  classDef information fill:#5c5c5c,color:#fff
```

### kind — tipo rígido que dá identidade

| conceito | definição | relações | normas | realizado por |
|---|---|---|---|---|
| **Parte**<br>`party` | Pessoa ou organização identificável que transaciona. Dá a identidade que comprador e vendedor emprestam. |  |  |  |
| **Item**<br>`item` | Objeto físico negociado. É ele que carrega os atributos conferidos na entrega e na disputa. |  |  |  |

### subkind — especialização rígida de um tipo

| conceito | definição | relações | normas | realizado por |
|---|---|---|---|---|
| **Pessoa física**<br>`person` | Parte identificada por CPF. | especializa party |  |  |
| **Pessoa jurídica**<br>`company` | Parte identificada por CNPJ, alfanumérico desde 2026. | especializa party |  |  |

### role — papel anti-rígido, depende de relação

| conceito | definição | relações | normas | realizado por |
|---|---|---|---|---|
| **Comprador**<br>`buyer` | Papel de quem publica a intenção e escolhe a proposta. Anti-rígido, a mesma parte vende noutra transação. | especializa party |  |  |
| **Vendedor**<br>`seller` | Papel de quem responde a uma intenção com proposta selada. | especializa party |  |  |
| **Agente de custódia**<br>`escrow-agent` | Instituição de pagamento autorizada que custodia o valor. A plataforma só comanda os gatilhos de liberação e estorno. | especializa company |  |  |
| **Árbitro**<br>`arbiter` | Papel de quem confere os campos probatórios numa disputa. | especializa party |  |  |

### relator — relator: existe entre duas ou mais partes

| conceito | definição | relações | normas | realizado por |
|---|---|---|---|---|
| **Acordo**<br>`agreement` | Contrato que nasce da seleção, com os compromissos mútuos de pagar e de entregar conforme os termos congelados. | media buyer, seller<br>fundado por proposal-chosen |  |  |
| **Escrow**<br>`escrow` | Custódia terceirizada do valor que libera ao vendedor ou estorna ao comprador por gatilho da plataforma. | media buyer, seller, escrow-agent<br>fundado por escrow-opened |  |  |

### commitment — compromisso social com validade

| conceito | definição | relações | normas | realizado por |
|---|---|---|---|---|
| **Proposta selada**<br>`proposal` | Compromisso condicional do vendedor com item de atributos declarados, preço total selado e validade explícita. |  |  |  |

### social-object — objeto social criado por ato de fala

| conceito | definição | relações | normas | realizado por |
|---|---|---|---|---|
| **Intenção de compra**<br>`intent` | Pedido público de propostas criado pelo comprador, com categoria, restrições sobre atributos e janela de propostas. Não obriga a comprar. |  |  |  |
| **Avaliação**<br>`rating` | Juízo de uma parte sobre a outra depois da transação, revelado às cegas. |  |  |  |

### type — tipo de alta ordem: instâncias são tipos

| conceito | definição | relações | normas | realizado por |
|---|---|---|---|---|
| **Categoria**<br>`category` | Tipo de alta ordem, suas instâncias são tipos de item (carta TCG, tênis, porta dianteira). Ancora no MCC e define as qualidades que o item carrega. | classifica item<br>parte de niche | iso-18245 |  |

### collective — coletivo de membros uniformes

| conceito | definição | relações | normas | realizado por |
|---|---|---|---|---|
| **Nicho**<br>`niche` | Coletivo de categorias com o mesmo regime regulatório e a mesma onda de lançamento (colecionáveis, moda circular, autopeças). |  |  |  |

### quality — qualidade que inere num portador

| conceito | definição | relações | normas | realizado por |
|---|---|---|---|---|
| **Atributo**<br>`attribute` | Qualidade do item valorada numa estrutura de qualidade tipada (enum, faixa, identificador normativo). É a espinha probatória do contrato. | inere em item |  |  |
| **Reputação**<br>`reputation` | Estimativa a posteriori da confiabilidade de uma parte num papel. Carrega a própria incerteza num intervalo de credibilidade. | inere em party |  |  |

### information — artefato de informação

| conceito | definição | relações | normas | realizado por |
|---|---|---|---|---|
| **Schema de atributos**<br>`attribute-schema` | Artefato versionado com as qualidades tipadas de uma categoria. Herda presets dos ancestrais na árvore. | parte de category |  |  |
| **Termos congelados**<br>`terms-snapshot` | Cópia canônica da intenção, da proposta e do schema no instante do acordo. O hash dela é a prova. | parte de agreement |  |  |
| **Reclamação de campo**<br>`field-claim` | Alegação sobre um atributo probatório, esperado contra observado, com evidência anexada. | parte de dispute |  |  |
| **Registro de evento**<br>`event-record` | Envelope imutável de um evento de domínio publicado pelo outbox. |  |  |  |

### datatype — valor sem identidade

| conceito | definição | relações | normas | realizado por |
|---|---|---|---|---|
| **Dinheiro**<br>`money` | Quantia em unidades menores de uma moeda. |  | iso-4217 |  |
| **Duração**<br>`duration` | Período nominal ou exato para janelas e validades. |  |  |  |
| **Identificador de entidade**<br>`entity-id` | Identificador ordenado no tempo, amigo de índice B-tree. |  |  |  |
| **MCC**<br>`mcc` | Código de categoria de comerciante que ancora a taxonomia curada. |  | iso-18245 |  |
| **CPF**<br>`cpf` | Identificador fiscal de pessoa física com dois dígitos verificadores. |  |  |  |
| **CNPJ**<br>`cnpj` | Identificador fiscal de pessoa jurídica, com base alfanumérica e dígitos verificadores numéricos. |  |  |  |
| **GTIN**<br>`gtin` | Identificador de produto com dígito verificador. |  |  |  |
| **ISBN**<br>`isbn` | Identificador de livro, nas formas de 10 e 13 dígitos. |  |  |  |
| **VIN (chassi)**<br>`vin` | Identificador do veículo, usado para conferir compatibilidade e origem da peça. |  |  |  |
| **RENAVAM**<br>`renavam` | Registro do veículo de origem de uma peça de desmonte. |  |  |  |
| **CEP**<br>`postal-code` | Código postal que resolve a UF de origem e de destino para estimar prazo. |  |  |  |
| **UF**<br>`state` | Unidade federativa, com região geográfica. |  | ibge-uf |  |
| **País**<br>`country` | País ou território, encadeado às regiões estatísticas. |  | iso-3166-1, un-m49 |  |
| **Idioma**<br>`language` | Idioma de um item, como a língua de impressão de uma carta. |  | iso-639 |  |
| **Moeda**<br>`currency` | Moeda com código alfabético, numérico e unidades menores. |  | iso-4217 |  |
| **Código de pagamento**<br>`payment-code` | Payload copia e cola com as instruções de pagamento do escrow. |  |  |  |

### phase — fase anti-rígida do ciclo de vida

| conceito | definição | relações | normas | realizado por |
|---|---|---|---|---|
| **Aberta**<br>`intent-open` | Recebendo propostas até o fechamento. | fase de intent |  |  |
| **Fechada**<br>`intent-closed` | Janela encerrada, propostas reveladas ao comprador e seleção pendente. | fase de intent |  |  |
| **Adjudicada**<br>`intent-awarded` | Uma proposta escolhida, virou acordo. | fase de intent |  |  |
| **Expirada**<br>`intent-expired` | Fechou sem seleção dentro do prazo. | fase de intent |  |  |
| **Cancelada**<br>`intent-cancelled` | Retirada pelo comprador antes da seleção. | fase de intent |  |  |
| **Ativa**<br>`proposal-active` | Válida e concorrendo. | fase de proposal |  |  |
| **Retirada**<br>`proposal-withdrawn` | Retirada pelo vendedor antes do fechamento. | fase de proposal |  |  |
| **Selecionada**<br>`proposal-selected` | Escolhida pelo comprador. | fase de proposal |  |  |
| **Preterida**<br>`proposal-rejected` | Outra proposta foi escolhida ou a intenção terminou sem ela. | fase de proposal |  |  |
| **Caducada**<br>`proposal-lapsed` | A validade venceu antes da seleção. | fase de proposal |  |  |
| **Aguardando pagamento**<br>`agreement-awaiting-funding` | Escrow aberto, esperando o comprador pagar. | fase de agreement |  |  |
| **Pago**<br>`agreement-funded` | Valor custodiado pelo agente, vendedor liberado para enviar. | fase de agreement |  |  |
| **Enviado**<br>`agreement-shipped` | Vendedor informou o envio. | fase de agreement |  |  |
| **Entregue**<br>`agreement-delivered` | Item chegou, janela de inspeção correndo. | fase de agreement |  |  |
| **Em disputa**<br>`agreement-disputed` | Comprador reclamou de campos probatórios dentro da inspeção. | fase de agreement |  |  |
| **Liberado**<br>`agreement-released` | Terminal. Valor liberado ao vendedor. | fase de agreement |  |  |
| **Estornado**<br>`agreement-refunded` | Terminal. Valor devolvido ao comprador. | fase de agreement |  |  |
| **Cancelado**<br>`agreement-cancelled` | Terminal. Pagamento não chegou no prazo. | fase de agreement |  |  |

### event — evento (perdurante)

| conceito | definição | relações | normas | realizado por |
|---|---|---|---|---|
| **Disputa**<br>`dispute` | Processo de conferência de campos probatórios entre o que o termo diz e o que chegou. | participantes buyer, seller, arbiter |  |  |
| **Intenção publicada**<br>`intent-published` | O comprador publica a intenção e abre a janela de propostas. | participantes buyer<br>cria intent |  |  |
| **Proposta enviada**<br>`proposal-submitted` | O vendedor sela e envia uma proposta. | participantes seller<br>cria proposal |  |  |
| **Proposta revisada**<br>`proposal-revised` | O vendedor troca a proposta ativa antes do fechamento. | participantes seller |  |  |
| **Proposta retirada**<br>`proposal-retracted` | O vendedor retira a proposta antes do fechamento. | participantes seller |  |  |
| **Janela fechada**<br>`bidding-closed` | O prazo vence sob o relógio do banco e os envelopes podem ser abertos. | participantes intent |  |  |
| **Proposta escolhida**<br>`proposal-chosen` | O comprador escolhe uma proposta entre as reveladas. | participantes buyer, proposal |  |  |
| **Acordo formado**<br>`agreement-formed` | Os termos são congelados e o acordo nasce. | participantes buyer, seller<br>cria agreement |  |  |
| **Escrow aberto**<br>`escrow-opened` | O agente de custódia abre a cobrança com as instruções de pagamento. | participantes escrow-agent, buyer<br>cria escrow |  |  |
| **Valor custodiado**<br>`escrow-held` | O pagamento caiu na custódia. | participantes escrow-agent, buyer |  |  |
| **Item enviado**<br>`item-shipped` | O vendedor despacha o item. | participantes seller, item |  |  |
| **Item entregue**<br>`item-delivered` | O item chega ao comprador e a inspeção começa. | participantes buyer, item |  |  |
| **Entrega aceita**<br>`delivery-accepted` | O comprador aceita, ou a inspeção vence sem reclamação. | participantes buyer |  |  |
| **Valor liberado**<br>`escrow-released` | O agente libera o valor ao vendedor. | participantes escrow-agent, seller |  |  |
| **Valor estornado**<br>`escrow-refunded` | O agente devolve o valor ao comprador. | participantes escrow-agent, buyer |  |  |
| **Disputa resolvida**<br>`dispute-resolved` | A conferência de campos decide entre liberar e estornar. | participantes arbiter |  |  |
| **Avaliação enviada**<br>`rating-submitted` | Uma parte avalia a outra, ainda selada. | participantes party<br>cria rating |  |  |
| **Avaliações reveladas**<br>`ratings-revealed` | As duas avaliações aparecem juntas, ou o prazo vence. | participantes buyer, seller |  |  |
| **Intenção expirada**<br>`selection-timed-out` | O prazo de seleção vence sem escolha. | participantes intent |  |  |
| **Intenção cancelada**<br>`intent-withdrawn` | O comprador retira a intenção. | participantes buyer |  |  |

## Normas

Cadeia taxonômica: autoridade → norma → edição → tipo de artefato → pacote → conceitos que se apoiam nela. A fundamentação mostrada é a do elo mais fraco entre as fontes: primária, secundária, reconstruída ou algorítmica.

| autoridade | norma | tipo | edição | fundamentação | estrutura | pacote | ancora |
|---|---|---|---|---|---|---|---|
| IBGE | **IBGE DTB — UF**<br>Divisão Territorial Brasileira: unidades da federação e grandes regiões (com ISO 3166-2:BR) | code-list | DTB vigente; códigos de UF estáveis desde a criação do Tocantins (1988) | reconstructed | tabelas de endereçamento direto (676 e 100 bytes); região derivada do primeiro dígito do código, sem tabela | `standards/ibge/uf` | state |
| ISO | **ISO 18245**<br>Retail financial services — Merchant category codes | classification | ISO 18245:2023; códigos e descrições de fontes públicas secundárias | reconstructed | o código é o valor; rank sobre 10 000 posições (1,25 KB) para pertinência e descrição; grupo por tabela de 100 bytes (código/100) | `standards/iso/iso18245` | category, mcc |
| ISO | **ISO 3166-1**<br>Codes for the representation of names of countries and their subdivisions — Part 1: Country codes | code-list | ISO 3166-1:2020 com boletins da agência de manutenção, via datasets/country-codes | secondary | três dicionários de rank (alpha-2, alpha-3, numérico) com permutação para a ordem dos registros; árvore M49 acima | `standards/iso/iso3166` | country |
| ISO | **ISO 4217**<br>Codes for the representation of currencies | code-list | ISO 4217:2015, lista da agência de manutenção publicada em 2026-09-17 | secondary | handle de 2 bytes; rank sobre 26³ chaves (2,7 KB) para o alfa e sobre 1000 para o numérico; strings fatiadas de constantes | `standards/iso/iso4217` | money, currency |
| ISO | **ISO 639-1/639-2**<br>Codes for the representation of names of languages | code-list | registro da Library of Congress (RA da ISO 639-2), via datasets/language-codes | secondary | rank sobre 26³ para o alfa-3 bibliográfico, rank + permutação para o terminológico e para o alfa-2 | `standards/iso/iso639` | language |
| UN | **UN M49**<br>Standard country or area codes for statistical use | classification | revisão vigente na UNSD, via datasets/country-codes | secondary | árvore implícita com numeração de Euler (pre/post): contenção em duas comparações; rank sobre 1000 códigos | `standards/un/m49` | country |

## Paradigmas

Cada pacote declara o paradigma que o governa (`//intenza:paradigm`). Os puros (núcleo funcional, autômatos, orientado a dados, probabilístico) não fazem I/O, não leem o relógio e não sorteiam sem uma fonte injetada; o verificador confere imports e referências.

| pacote | paradigma | puro |
|---|---|---|
| `cmd/intenza-kb` | casca imperativa | não |
| `internal/kb` | núcleo funcional | sim |
| `internal/kb/source` | casca imperativa | não |
| `internal/platform/buildinfo` | casca imperativa | não |
| `standards` | núcleo funcional | sim |
| `standards/ibge/uf` | orientado a dados | sim |
| `standards/internal/fold` | orientado a dados | sim |
| `standards/internal/gen` | generativo | não |
| `standards/internal/succinct` | orientado a dados | sim |
| `standards/iso/iso18245` | orientado a dados | sim |
| `standards/iso/iso3166` | orientado a dados | sim |
| `standards/iso/iso4217` | orientado a dados | sim |
| `standards/iso/iso639` | orientado a dados | sim |
| `standards/meta` | núcleo funcional | sim |
| `standards/un/m49` | orientado a dados | sim |

## Achados do verificador

- `warning` standard:ibge-uf: fonte reconstruída: precisa de revisão de especialista
- `warning` standard:iso-18245: fonte reconstruída: precisa de revisão de especialista

