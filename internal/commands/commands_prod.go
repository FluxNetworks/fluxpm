// +build prod

package commands

import (
	"github.com/FluxNetworks/fluxpm/internal/migrations"
)

func init() {
	migration = migrations.Migrations
}
