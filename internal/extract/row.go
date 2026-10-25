// Package extract deriva as funções IFPUG (dados e transacionais) do código.
package extract

import "fpcount/internal/ifpug"

// Conf é a confiança da classificação de uma linha.
type Conf string

const (
	Alta  Conf = "alta"
	Media Conf = "media"
	Baixa Conf = "baixa"
)

// Escopo da fronteira de contagem.
const (
	Nucleo   = "nucleo"
	Satelite = "satelite"
	Infra    = "infra" // listado, mas fora da contagem (rota de infraestrutura)
)

// Row é uma função contada (uma linha de contagem.csv).
type Row struct {
	ID       string
	Type     ifpug.Type
	Name     string
	DET      int
	Ref      int // RET (dados) ou FTR (transacionais)
	Comp     ifpug.Complexity
	PF       int
	Conf     Conf
	Review   bool
	Evidence string
	Scope    string
	Note     string
	// Defaulted marca DET/Ref que não puderam ser extraídos (usa-se valor
	// neutro e a complexidade entra no cenário de sensibilidade).
	Defaulted bool
}

// Finish calcula complexidade e PF a partir de DET/Ref.
func (r *Row) Finish() {
	switch r.Type {
	case ifpug.ILF, ifpug.EIF:
		r.Comp = ifpug.DataComplexity(r.Ref, r.DET)
	default:
		r.Comp = ifpug.TransComplexity(r.Type, r.Ref, r.DET)
	}
	r.PF = ifpug.Weight(r.Type, r.Comp)
	if r.Scope == Infra {
		r.PF = 0
	}
	r.Review = r.Conf == Baixa
}
