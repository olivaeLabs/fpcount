package ifpug

import "testing"

func TestDataComplexity(t *testing.T) {
	cases := []struct {
		ret, det int
		want     Complexity
	}{
		{1, 1, Baixa}, {1, 19, Baixa}, {1, 20, Baixa}, {1, 50, Baixa}, {1, 51, Media},
		{2, 19, Baixa}, {2, 20, Media}, {5, 50, Media}, {5, 51, Alta},
		{6, 19, Media}, {6, 20, Alta}, {9, 80, Alta},
	}
	for _, c := range cases {
		if got := DataComplexity(c.ret, c.det); got != c.want {
			t.Errorf("DataComplexity(ret=%d, det=%d) = %v, quer %v", c.ret, c.det, got, c.want)
		}
	}
}

func TestTransComplexityEI(t *testing.T) {
	cases := []struct {
		ftr, det int
		want     Complexity
	}{
		{0, 1, Baixa}, {1, 15, Baixa}, {1, 16, Media},
		{2, 4, Baixa}, {2, 5, Media}, {2, 16, Alta},
		{3, 4, Media}, {3, 5, Alta}, {7, 30, Alta},
	}
	for _, c := range cases {
		if got := TransComplexity(EI, c.ftr, c.det); got != c.want {
			t.Errorf("EI(ftr=%d, det=%d) = %v, quer %v", c.ftr, c.det, got, c.want)
		}
	}
}

func TestTransComplexityEOEQ(t *testing.T) {
	cases := []struct {
		ftr, det int
		want     Complexity
	}{
		{1, 5, Baixa}, {1, 19, Baixa}, {1, 20, Media},
		{2, 5, Baixa}, {3, 6, Media}, {3, 20, Alta},
		{4, 5, Media}, {4, 6, Alta}, {6, 25, Alta},
	}
	for _, typ := range []Type{EO, EQ} {
		for _, c := range cases {
			if got := TransComplexity(typ, c.ftr, c.det); got != c.want {
				t.Errorf("%v(ftr=%d, det=%d) = %v, quer %v", typ, c.ftr, c.det, got, c.want)
			}
		}
	}
}

func TestWeight(t *testing.T) {
	cases := []struct {
		typ  Type
		c    Complexity
		want int
	}{
		{ILF, Baixa, 7}, {ILF, Media, 10}, {ILF, Alta, 15},
		{EIF, Baixa, 5}, {EIF, Media, 7}, {EIF, Alta, 10},
		{EI, Baixa, 3}, {EI, Media, 4}, {EI, Alta, 6},
		{EO, Baixa, 4}, {EO, Media, 5}, {EO, Alta, 7},
		{EQ, Baixa, 3}, {EQ, Media, 4}, {EQ, Alta, 6},
	}
	for _, c := range cases {
		if got := Weight(c.typ, c.c); got != c.want {
			t.Errorf("Weight(%v,%v) = %d, quer %d", c.typ, c.c, got, c.want)
		}
	}
}
