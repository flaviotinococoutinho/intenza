# standards

SDKs de referências normativas em Go puro, sem dependência fora da biblioteca padrão. É um módulo próprio (`github.com/flaviotinococoutinho/intenza/standards`): dá para importar sem puxar Mongo, Kafka ou Redis.

## Taxonomia

Cada norma entra numa cadeia: **autoridade → norma → edição → tipo de artefato → pacote**. O tipo de artefato decide o paradigma e a estrutura de dados.

| autoridade | norma | pacote | tipo |
|---|---|---|---|
| ISO | ISO 18245 (MCC) | `iso/iso18245` | classificação |
| ISO | ISO 4217 (moedas) | `iso/iso4217` | lista de códigos |
| ISO | ISO 3166-1 (países) | `iso/iso3166` | lista de códigos |
| ISO | ISO 639-1/639-2 (idiomas) | `iso/iso639` | lista de códigos |
| UN | M49 (áreas estatísticas) | `un/m49` | classificação |
| IBGE | DTB, unidades da federação | `ibge/uf` | lista de códigos |
| GS1 | GTIN e GS1 Prefixes | `gs1/gtin` | identificador |
| ISO | ISO 2108 (ISBN) | `iso/iso2108` | identificador |
| ISO | ISO 3779/3780 (VIN, WMI) | `iso/iso3779` | identificador |
| RFB | CPF | `rfb/cpf` | identificador |
| RFB | CNPJ alfanumérico (IN RFB 2.229/2024) | `rfb/cnpj` | identificador |
| SENATRAN | RENAVAM | `senatran/renavam` | identificador |
| Correios | CEP → UF | `correios/cep` | mapa de faixas |
| ISO | ISO 8601-1 (durações) | `iso/iso8601` | gramática |
| IETF | RFC 8785 (JSON canônico) | `ietf/rfc8785` | gramática |
| IETF | RFC 9562 (UUIDv7) | `ietf/rfc9562` | gramática |
| EMVCo | QRCPS-MPM | `emvco/mpm` | gramática |
| BCB | BR Code (Pix) | `bcb/brcode` | gramática |

O BR Code mostra a cadeia em funcionamento: o payload do Pix é um perfil EMV MPM cujo campo 52 é um MCC (ISO 18245), o 53 é a moeda 986 (ISO 4217) e o 58 é o país BR (ISO 3166-1). Os imports do pacote são essa frase.

| tipo de artefato | paradigma | estrutura |
|---|---|---|
| lista de códigos | orientado a dados | chave → inteiro denso (base 26 ou decimal) → dicionário de rank (bitset + popcount) → arrays paralelos |
| classificação | orientado a dados | árvore implícita com numeração de Euler: contenção em duas comparações |
| identificador | funcional | dígito verificador por tabela, com prova exaustiva do que ele detecta |
| gramática | autômatos | scanner escrito à mão, sem regexp, sem alocação no caminho quente |
| mapa de faixas | orientado a dados | tabela direta no prefixo significativo |

## Estruturas de consulta

O dado é estático, então tudo que dá para calcular antes vira código gerado:

- **Dicionário de rank.** Um código de três letras vira um inteiro em [0, 17 576). Um bit por chave possível mais a contagem acumulada por palavra de 64 bits dá a posição do registro com uma leitura, uma máscara e um `popcount`. São 2,7 KB, sem hash.
- **Sem ponteiros.** Nomes ficam numa string constante com offsets em `uint32`; os registros são arrays paralelos de inteiros. O GC não tem o que varrer e os acessores fatiam constantes sem alocar.
- **Busca por prefixo.** Índice invertido estático de tokens dobrados (sem acento e sem caixa), busca binária no dicionário de termos e interseção com um bitset que mora na pilha.

Resultados registrados em Apple M2 com `make bench`. Os valores descrevem essa medição, não um limite garantido em outras máquinas:

| operação | ns/op | alocações |
|---|---:|---:|
| `iso4217.ByAlpha` (rank) | 4,5 | 0 |
| o mesmo com `map[string]` | 9,5 | 0 |
| `iso3166.Country.In(área M49)` | 7,3 | 0 |
| `iso18245.MCC.Description` | 3,3 | 0 |
| `iso4217.Search("real")` | 72 | 0 |
| `iso8601.Parse("P1Y2M10DT2H30M0.25S")` | 53 | 0 |

## Rastreabilidade das fontes e dos testes

Cada pacote expõe `Spec()` com as fontes, a **fundamentação** da cópia (primária, secundária, reconstruída ou algorítmica) e como a conformidade é verificada. A corrente de justificação é explícita:

1. o cabeçalho do extrato em `data/*.csv` guarda a URL e o sha256 do arquivo da autoridade;
2. o código gerado guarda o sha256 do extrato;
3. um teste recalcula esse sha256 e falha se alguém editar o extrato sem regenerar;
4. testes exaustivos fazem ida e volta por todas as chaves de todos os registros.

`meta.Standard.Weakest()` identifica a fundamentação mais fraca entre as dependências. A base de conhecimento registra quando uma fonte é reconstruída.

## Erros detectados pelos dígitos verificadores

A tabela distingue resultados de testes exaustivos e de amostragem. Um dígito verificador detecta certas alterações no identificador; ele não comprova que o registro existe:

| identificador | substituição de um caractere | troca de vizinhos | como a gente sabe |
|---|---|---|---|
| GTIN (mod 10, pesos 3/1) | todas | todas, exceto dígitos que diferem em 5 (05 ↔ 50) | prova exaustiva |
| ISBN-10 (mod 11, pesos 10..1) | todas | todas | prova exaustiva |
| VIN (mod 11, 49 CFR 565) | todas, exceto letras com a mesma transliteração (A e J valem 1) | — | prova exaustiva |
| CPF (mod 11, resto 0/1 → 0) | ~99,8% | — | medido por amostragem |
| CNPJ alfanumérico | mesma regra do CPF, valor = ASCII − 48 | — | coincide com a regra antiga em toda base numérica |

O prefixo GS1 diz quem emitiu o GTIN, não onde o produto foi feito: a própria GS1 avisa.

## Atualizar uma norma

Execute a partir da raiz do repositório. O gerador pertence ao módulo `standards`. Os downloads abaixo requerem acesso à rede.

```bash
cd standards
mkdir -p /tmp/intenza-normas
# Baixe as fontes no diretório criado.
curl -sSLo /tmp/intenza-normas/iso4217-list-one.xml https://www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml
curl -sSLo /tmp/intenza-normas/cldr-pt.xml https://raw.githubusercontent.com/unicode-org/cldr/main/common/main/pt.xml
curl -sSLo /tmp/intenza-normas/country-codes.csv https://raw.githubusercontent.com/datasets/country-codes/main/data/country-codes.csv
curl -sSLo /tmp/intenza-normas/language-codes-full.csv https://raw.githubusercontent.com/datasets/language-codes/main/data/language-codes-full.csv
curl -sSLo /tmp/intenza-normas/mcc_codes.csv https://raw.githubusercontent.com/greggles/mcc-codes/main/mcc_codes.csv

go run ./internal/gen extract iso4217 /tmp/intenza-normas   # idem: iso3166 (+m49), iso639, iso18245
go generate ./...
git diff -- iso/ un/                              # confira os extratos e as tabelas geradas
```

Atribuições e licenças das fontes em [NOTICE.md](NOTICE.md).
