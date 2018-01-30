package swarm

import (
	"strings"

	"bitbucket.org/stack-rox/apollo/generated/api/v1"
	"bitbucket.org/stack-rox/apollo/pkg/checks/utils"
	"bitbucket.org/stack-rox/apollo/pkg/docker"
	"github.com/docker/docker/api/types"
)

type minimumManagers struct{}

func (c *minimumManagers) Definition() utils.Definition {
	return utils.Definition{
		CheckDefinition: v1.CheckDefinition{
			Name:        "CIS Docker v1.1.0 - 7.2",
			Description: "Ensure the minimum number of manager nodes have been created in a swarm",
		},
		Dependencies: []utils.Dependency{utils.InitDockerClient},
	}
}

func (c *minimumManagers) Run() (result v1.CheckResult) {
	if !utils.DockerInfo.Swarm.ControlAvailable {
		utils.NotApplicable(&result)
		utils.AddNotes(&result, "Checking minimum managers applies only to Swarm managers and this node is not a Swarm Manager")
		return
	}

	utils.Note(&result)
	ctx, cancel := docker.TimeoutContext()
	defer cancel()
	nodeList, err := utils.DockerClient.NodeList(ctx, types.NodeListOptions{})
	if err != nil {
		utils.Info(&result)
		utils.AddNotef(&result, "Could not get node list: %+v", err)
		return
	}
	var leaders, managers, workers []string
	for _, node := range nodeList {
		if node.ManagerStatus == nil {
			workers = append(workers, node.ID)
		} else if node.ManagerStatus.Leader {
			leaders = append(leaders, node.ID)
			managers = append(managers, node.ID)
		} else {
			managers = append(leaders, node.ID)
		}
	}
	utils.AddNotef(&result, "Current Manager configuration: Leaders (%v). Managers (%v). Total workers %v", strings.Join(leaders, ","), strings.Join(managers, ","), len(workers))
	return
}

// NewMinimumManagersCheck implements CIS-7.1
func NewMinimumManagersCheck() utils.Check {
	return &minimumManagers{}
}
