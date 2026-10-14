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

| tipo de artefato | paradigma | estrutura |
|---|---|---|
| lista de códigos | orientado a dados | chave → inteiro denso (base 26 ou decimal) → dicionário de rank (bitset + popcount) → arrays paralelos |
| classificação | orientado a dados | árvore implícita com numeração de Euler: contenção em duas comparações |
| identificador | funcional | dígito verificador por tabela, com prova exaustiva do que ele detecta |
| gramática | autômatos | scanner escrito à mão, sem regexp, sem alocação no caminho quente |
| mapa de faixas | orientado a dados | tabela direta no prefixo significativo |

## Por que é rápido

O dado é estático, então tudo que dá para calcular antes vira código gerado:

- **Dicionário de rank.** Um código de três letras vira um inteiro em [0, 17 576). Um bit por chave possível mais a contagem acumulada por palavra de 64 bits dá a posição do registro com uma leitura, uma máscara e um `popcount`. São 2,7 KB, sem hash.
- **Sem ponteiros.** Nomes ficam numa string constante com offsets em `uint32`; os registros são arrays paralelos de inteiros. O GC não tem o que varrer e os acessores fatiam constantes sem alocar.
- **Busca por prefixo.** Índice invertido estático de tokens dobrados (sem acento e sem caixa), busca binária no dicionário de termos e interseção com um bitset que mora na pilha.

Apple M2, `make bench`:

| operação | ns/op | alocações |
|---|---:|---:|
| `iso4217.ByAlpha` (rank) | 4,5 | 0 |
| o mesmo com `map[string]` | 9,5 | 0 |
| `iso3166.Country.In(área M49)` | 7,3 | 0 |
| `iso18245.MCC.Description` | 3,3 | 0 |
| `iso4217.Search("real")` | 72 | 0 |

## Como a gente sabe que está certo

Cada pacote expõe `Spec()` com as fontes, a **fundamentação** da cópia (primária, secundária, reconstruída ou algorítmica) e como a conformidade é verificada. A corrente de justificação é explícita:

1. o cabeçalho do extrato em `data/*.csv` guarda a URL e o sha256 do arquivo da autoridade;
2. o código gerado guarda o sha256 do extrato;
3. um teste recalcula esse sha256 e falha se alguém editar o extrato sem regenerar;
4. testes exaustivos fazem ida e volta por todas as chaves de todos os registros.

A cadeia vale o elo mais fraco: `meta.Standard.Weakest()` diz qual é, e a base de conhecimento do projeto avisa quando uma fonte é reconstruída.

## Atualizar uma norma

```bash
# baixe as fontes num diretório
curl -sSLo /tmp/src/iso4217-list-one.xml https://www.six-group.com/dam/download/financial-information/data-center/iso-currrency/lists/list-one.xml
curl -sSLo /tmp/src/cldr-pt.xml https://raw.githubusercontent.com/unicode-org/cldr/main/common/main/pt.xml
curl -sSLo /tmp/src/country-codes.csv https://raw.githubusercontent.com/datasets/country-codes/main/data/country-codes.csv
curl -sSLo /tmp/src/language-codes-full.csv https://raw.githubusercontent.com/datasets/language-codes/main/data/language-codes-full.csv
curl -sSLo /tmp/src/mcc_codes.csv https://raw.githubusercontent.com/greggles/mcc-codes/main/mcc_codes.csv

go run ./internal/gen extract iso4217 /tmp/src   # idem: iso3166 (+m49), iso639, iso18245
go generate ./...
git diff data/                                    # o que a autoridade mudou
```

Atribuições e licenças das fontes em [NOTICE.md](NOTICE.md).
