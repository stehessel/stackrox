package containerruntime

import (
	"bitbucket.org/stack-rox/apollo/generated/api/v1"
	"bitbucket.org/stack-rox/apollo/pkg/checks/utils"
)

type sharedNetworkBenchmark struct{}

func (c *sharedNetworkBenchmark) Definition() utils.Definition {
	return utils.Definition{
		CheckDefinition: v1.CheckDefinition{
			Name:        "CIS Docker v1.1.0 - 5.9",
			Description: "Ensure the host's network namespace is not shared",
		}, Dependencies: []utils.Dependency{utils.InitContainers},
	}
}

func (c *sharedNetworkBenchmark) Run() (result v1.CheckResult) {
	utils.Pass(&result)
	for _, container := range utils.ContainersRunning {
		if container.HostConfig.NetworkMode.IsHost() {
			utils.Warn(&result)
			utils.AddNotef(&result, "Container '%v' has network set to --net=host", container.ID)
		}
	}
	return
}

// NewSharedNetworkBenchmark implements CIS-5.9
func NewSharedNetworkBenchmark() utils.Check {
	return &sharedNetworkBenchmark{}
}
