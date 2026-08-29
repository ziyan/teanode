//go:build ignore

package main

import (
	"flag"

	"github.com/ziyan/teanode/internal/util/commentparse"
)

func main() {
	flag.Parse()
	comments := commentparse.Parse("github.com/ziyan/teanode/internal/...", false)
	commentparse.MustWriteGeneratedCode(comments)
}
