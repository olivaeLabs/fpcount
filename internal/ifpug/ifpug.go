// Package ifpug implementa as matrizes de complexidade e os pesos do IFPUG
// CPM 4.3.1 (contagem de pontos de função não ajustados).
package ifpug

// Type é o tipo de função do IFPUG.
type Type string

const (
	ILF Type = "ILF"
	EIF Type = "EIF"
	EI  Type = "EI"
	EO  Type = "EO"
	EQ  Type = "EQ"
)

// Complexity é a complexidade funcional.
type Complexity string

const (
	Baixa Complexity = "baixa"
	Media Complexity = "media"
	Alta  Complexity = "alta"
)

// CPMVersion é a versão do manual cujas tabelas este pacote implementa.
const CPMVersion = "4.3.1"

func band(n int, lo, hi int) int {
	switch {
	case n < lo:
		return 0
	case n <= hi:
		return 1
	default:
		return 2
	}
}

func lookup(g [3][3]Complexity, r, c int) Complexity { return g[r][c] }

// DataComplexity classifica ILF/EIF por RET (linhas) e DET (colunas 1-19, 20-50, 51+).
func DataComplexity(ret, det int) Complexity {
	g := [3][3]Complexity{
		{Baixa, Baixa, Media},
		{Baixa, Media, Alta},
		{Media, Alta, Alta},
	}
	r := 0
	switch {
	case ret >= 6:
		r = 2
	case ret >= 2:
		r = 1
	}
	c := 0
	switch {
	case det >= 51:
		c = 2
	case det >= 20:
		c = 1
	}
	return lookup(g, r, c)
}

// TransComplexity classifica EI/EO/EQ por FTR e DET.
func TransComplexity(t Type, ftr, det int) Complexity {
	if t == EI {
		g := [3][3]Complexity{
			{Baixa, Baixa, Media},
			{Baixa, Media, Alta},
			{Media, Alta, Alta},
		}
		r := 0
		switch {
		case ftr > 2:
			r = 2
		case ftr == 2:
			r = 1
		}
		return lookup(g, r, band(det, 5, 15))
	}
	g := [3][3]Complexity{
		{Baixa, Baixa, Media},
		{Baixa, Media, Alta},
		{Media, Alta, Alta},
	}
	r := 0
	switch {
	case ftr >= 4:
		r = 2
	case ftr >= 2:
		r = 1
	}
	c := 0
	switch {
	case det >= 20:
		c = 2
	case det >= 6:
		c = 1
	}
	return lookup(g, r, c)
}

// Weight devolve os pontos de função do tipo na complexidade dada.
func Weight(t Type, c Complexity) int {
	w := map[Type][3]int{
		ILF: {7, 10, 15}, EIF: {5, 7, 10},
		EI: {3, 4, 6}, EO: {4, 5, 7}, EQ: {3, 4, 6},
	}[t]
	switch c {
	case Baixa:
		return w[0]
	case Media:
		return w[1]
	default:
		return w[2]
	}
}
