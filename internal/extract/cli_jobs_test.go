package extract

import "testing"

const cliFixture = `package main

import "flag"

func runMain(args []string) int {
	switch args[0] {
	case "serve":
		return 0
	case "list":
		return runList(args[1:])
	case "sync":
		return runSync(args[1:])
	}
	return 2
}

func runList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	fs.String("status", "", "x")
	fs.Bool("all", false, "y")
	return 0
}

func runSync(args []string) int { return 0 }
`

func TestCLIRowsSkipsRolesAndCountsFlags(t *testing.T) {
	ix := fixtureIndex(t, map[string]string{"go.mod": "module m\n", "cmd/tool/main.go": cliFixture})
	rows := CLIRows(ix, "cmd/tool", "tool", Nucleo)
	if len(rows) != 2 {
		t.Fatalf("linhas = %d (%v), quer 2 (serve é papel de processo)", len(rows), names(rows))
	}
	var list, sync Row
	for _, r := range rows {
		switch r.Name {
		case "tool list":
			list = r
		case "tool sync":
			sync = r
		}
	}
	if list.Type != "EQ" || list.DET != 2 {
		t.Errorf("list = %+v, quer EQ com DET=2 (flags)", list)
	}
	if sync.Type != "EI" || sync.DET != neutralDET || !sync.Review {
		t.Errorf("sync = %+v, quer EI neutro com revisão humana", sync)
	}
}

func TestJobRowsOneEIPerJobName(t *testing.T) {
	src := "package job\nconst dailyJobName = \"daily\"\nconst otherJobName = \"other\"\n"
	ix := fixtureIndex(t, map[string]string{"go.mod": "module m\n", "job/job.go": src})
	rows := JobRows(ix)
	if len(rows) != 2 || rows[0].Type != "EI" || !rows[0].Review {
		t.Fatalf("rows = %+v", rows)
	}
}
