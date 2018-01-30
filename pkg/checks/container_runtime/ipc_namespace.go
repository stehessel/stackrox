package containerruntime

import (
	"bitbucket.org/stack-rox/apollo/generated/api/v1"
	"bitbucket.org/stack-rox/apollo/pkg/checks/utils"
)

type ipcNamespaceBenchmark struct{}

func (c *ipcNamespaceBenchmark) Definition() utils.Definition {
	return utils.Definition{
		CheckDefinition: v1.CheckDefinition{
			Name:        "CIS Docker v1.1.0 - 5.16",
			Description: "Ensure the host's IPC namespace is not shared",
		}, Dependencies: []utils.Dependency{utils.InitContainers},
	}
}

func (c *ipcNamespaceBenchmark) Run() (result v1.CheckResult) {
	utils.Pass(&result)
	for _, container := range utils.ContainersRunning {
		if container.HostConfig.IpcMode.IsHost() {
			utils.Warn(&result)
			utils.AddNotef(&result, "Container %v has ipc mode set to host", container.ID)
		}
	}
	return
}

// NewIpcNamespaceBenchmark implements CIS-5.16
func NewIpcNamespaceBenchmark() utils.Check {
	return &ipcNamespaceBenchmark{}
}
