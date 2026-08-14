package main

import (
	"os"

	"github.com/barats/xlistman/cmd"
)

func main() {
	os.Exit(cmd.Run(os.Args[1:]))
}
