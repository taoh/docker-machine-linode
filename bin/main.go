package main

import (
	"github.com/docker/machine/libmachine/drivers/plugin"
	linode "github.com/taoh/docker-machine-linode"
)

func main() {
	plugin.RegisterDriver(linode.NewDriver("", ""))
}
