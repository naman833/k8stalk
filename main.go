package main

import (
	"os"

	"github.com/naman833/k8stalk/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
