package main

import (
	"os"

	"github.com/naman833/k8stalk/cmd"
)

// version is set at build time via -ldflags "-X main.version=...".
// It defaults to "dev" for plain `go build` invocations without ldflags.
var version = "dev"

func main() {
	cmd.SetVersion(version)
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
