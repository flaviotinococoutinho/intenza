# Intenza

Laboratório em Go de um marketplace reverso C2B. O comprador publica uma intenção de compra com atributos tipados, vendedores enviam propostas seladas e o comprador seleciona uma proposta para formar um contrato.

O repositório contém domínio, API HTTP, relay de eventos, workers e um simulador de provedor de pagamento. MongoDB, Kafka e Redis fazem parte da infraestrutura local. O simulador permite estudar o fluxo de escrow; ele não representa uma integração com pagamento real.

## Verificar o código

Use a versão de Go declarada em [go.mod](go.mod), atualmente `1.27.2`, e GNU Make. Execute os comandos na raiz do repositório:

```bash
make help
make test
make kb
```

`make test` executa os testes dos módulos encontrados pelo Makefile. `make kb` verifica os vínculos da base de conhecimento com o código. Esses comandos podem baixar dependências Go na primeira execução.

Para executar a análise estática, os testes com detector de concorrência e a verificação do código gerado, use `make check`. Os testes de integração exigem Docker e rodam com `make test-integration`.

## Estado da execução em containers

[deploy/compose.yaml](deploy/compose.yaml) descreve MongoDB, Redis, Kafka, Toxiproxy, API, relay, worker, simulador de pagamento e Prometheus. O profile `obs` acrescenta Grafana e Jaeger.

Há um bloqueio no build local: o Compose referencia `deploy/Dockerfile.local`, mas esse arquivo não está no repositório. Por isso, `make up` e `make up-obs` não são procedimentos completos de instalação no estado atual. Os comandos HTTP e as portas abaixo descrevem a configuração, sem comprovar uma execução bem-sucedida.

| Componente | Endereço configurado no host |
|---|---|
| API | `http://localhost:8080` |
| Administração e métricas da API | `http://localhost:9091` |
| Simulador de pagamento | `http://localhost:8090` |
| Prometheus | `http://localhost:9090` |
| Grafana, profile `obs` | `http://localhost:3000` |
| Jaeger, profile `obs` | `http://localhost:16686` |

O alvo `make down` usa `docker compose down -v` e apaga os volumes do laboratório.

## Consultar as referências

- [Base de conhecimento](docs/knowledge.md): ontologia, alegações e riscos. O arquivo é gerado; altere os dados em `knowledge/` e execute `go run ./cmd/intenza-kb render` para regenerá-lo.
- [SDK de normas](standards/README.md): módulo Go independente com códigos, identificadores e parsers.
- [Configuração dos serviços](internal/platform/config/config.go): variáveis, tipos e valores padrão.
- [Rotas HTTP](internal/adapter/httpapi/routes.go): operações expostas pela API.
- [Makefile](Makefile): comandos de validação, geração e infraestrutura.

Busca estruturada, reputação e escrow fazem parte do modelo do produto. O código e seus testes definem o que está implementado; a base de conhecimento distingue evidências e conjecturas.
