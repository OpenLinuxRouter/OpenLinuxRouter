package qos

import "github.com/open-linux-router/open-linux-router/internal/core"

func Dependencies() []core.Dependency {
	return []core.Dependency{{
		Tool: "tc", Why: "Client priorities and speed limits need Linux traffic control.", Inert: true,
		Packages: []core.Package{{Distro: "debian", Name: "iproute2"}, {Distro: "fedora", Name: "iproute-tc"}, {Distro: "rhel", Name: "iproute-tc"}, {Distro: "suse", Name: "iproute2"}, {Distro: "alpine", Name: "iproute2-tc"}, {Distro: "arch", Name: "iproute2"}},
	}}
}
