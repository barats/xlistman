package main

import (
	"embed"
	"os"

	"github.com/barats/xlistman/cmd"
)

//go:embed all:web/build
var webBuild embed.FS

func main() {
	os.Exit(cmd.Run(os.Args[1:], webBuild))
}
