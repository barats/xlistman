package main

import (
	"os"

	"github.com/barat/xlistman/cmd"
)

func main() {
	os.Exit(cmd.Run(os.Args[1:]))
}
