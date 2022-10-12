package main

import (
	"github.com/FluxNetworks/fluxpm/internal/commands"
	_ "github.com/lib/pq"
)

func main() {
	commands.Execute()
}
