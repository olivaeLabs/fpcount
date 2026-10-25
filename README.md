# fpcount

Contagem de pontos de função **IFPUG CPM 4.3.1** (não ajustados) por análise estática de código Go. Stdlib apenas (`go/parser`, `go/ast`); sem LLM; saída reprodutível byte a byte. Somente lê o alvo.

```bash
go run . count --root /caminho/do/repo --out /caminho/saida [--satellite /outro/repo]...
```

Gera `contagem.csv` (uma linha por função: id, escopo, tipo, DET, RET/FTR, complexidade, PF, confiança, `revisao_humana`, evidência `arquivo:linha`), `memoria-de-calculo.md` (método, fronteira, totais, sensibilidade, tabelas de decisão) e `sumario.json`.

## O que é contado
| Tipo | Origem | Regra |
|---|---|---|
| ILF | constantes `*Collection*` (MongoDB) | agrupamento por `DefaultMerges`; DET = campos-folha `bson`/`json`; RET = tipos-struct distintos |
| EIF | tabela `DefaultEIFs` | DET/RET = structs só-`json` do pacote |
| EI/EO/EQ | rotas de `internal/httpapi` (`Route{}`, `registry.Register`, wrappers; alias `/api/v1` deduplicado) | GET sem derivação = EQ; GET com derivação = EO; escrita = EI; FTR = campos de dados tocados pelo handler |
| EI | constantes `*JobName` | FTR por dependências do arquivo; DET neutro |
| EI/EQ | folhas de subcomandos (`switch args[0]`) | DET = flags; FTR neutro |

Fora: rotas de infra (PF=0), MCP, telas, test-doubles. Satélites saem em escopo separado e **nunca** somados ao núcleo; para eles só há pontos de entrada de CLI (piso).

## Limites (leia antes de citar um número)
É **estimativa funcional preliminar automatizada**, não contagem certificada. Linhas `revisao_humana=true` e as "tabelas de decisão" da memória de cálculo precisam de conferência manual (agrupamento de ILF, fronteira de processo elementar, FTR real). Valores neutros (DET/FTR não extraíveis) entram no cenário de sensibilidade.

## Testes
```bash
go vet ./... && go test ./...
```
Mutações verificadas na criação: alterar o peso da EI e fazer o campo `logger` contar como FTR derrubam testes.

## Propagação
As tabelas padrão de EIF e de agrupamento de coleções (`internal/extract/eif.go`, `data.go`) são exemplos: ajuste-as ao projeto analisado.
