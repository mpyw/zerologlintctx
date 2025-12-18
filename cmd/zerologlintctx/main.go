// Command zerologlintctx is a linter that checks for proper context propagation in zerolog logging chains.
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/mpyw/zerologlintctx"
)

func main() {
	singlechecker.Main(analyzer.Analyzer)
}
