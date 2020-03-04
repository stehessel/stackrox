package filter

import (
	"strings"

	"github.com/stackrox/rox/generated/storage"
	"github.com/stackrox/rox/pkg/set"
	"github.com/stackrox/rox/pkg/stringutils"
	"github.com/stackrox/rox/pkg/sync"
)

// This filter is a rudimentary filter that prevents a container from spamming Central
//
// Parameters:
// maxExactPathMatch:
// 	The maximum number of times a complete path has been taken
// 	e.g. The exact path "bash -c nmap" will only be passed through at most maxExactPathMatch
//
// fanOut:
// 	The degree of fan out of each level, generally decreasing
//
// Logic:
// 	Keyed on deployment -> container ID, define a root level. Take the exec file path and retrieve or create
// 	the level for that specific process. No process exec file paths are limited because we want to see all new binaries.
// 	Then recursively sift through the args and create a level for each argument (up to len(fanOut)) that has a parent of the
//  previous argument. If the fan out or the number of maxExactPathMatches has been exceeded, then return false. Otherwise, return true

const (
	maxArgSize = 16
)

// Filter takes in a process indicator via add and determines if should be filtered or not
//go:generate mockgen-wrapper
type Filter interface {
	Add(indicator *storage.ProcessIndicator) bool
	Update(deployment *storage.Deployment)
	UpdateByGivenContainers(deploymentID string, liveContainerSet set.StringSet)
	Delete(deploymentID string)
}

type level struct {
	hits     int
	children map[string]*level
}

func newLevel() *level {
	return &level{
		children: make(map[string]*level),
	}
}

type filterImpl struct {
	maxExactPathMatches int   // maximum number of exact path (same pod + container and same process and args) matches to tolerate
	maxFanOut           []int // maximum fan out starting at the process level

	containersInDeployment map[string]map[string]*level
	rootLock               sync.Mutex
}

func (f *filterImpl) siftNoLock(level *level, args []string, levelNum int) bool {
	if len(args) == 0 || levelNum >= len(f.maxFanOut) {
		// If we have hit this point with this exact level structure, maxExactPathMatch number of times
		// then return false. Otherwise increment the levels hits and return true
		if level.hits >= f.maxExactPathMatches {
			return false
		}
		level.hits++
		return true
	}
	// Truncate the current argument to the max size to avoid large arguments taking up a lot of space
	currentArg := stringutils.Truncate(args[0], maxArgSize)
	nextLevel := level.children[currentArg]
	if nextLevel == nil {
		// If this level has already hit its max fan out then return false
		if len(level.children) >= f.maxFanOut[levelNum] {
			return false
		}
		nextLevel = newLevel()
		level.children[currentArg] = nextLevel
	}

	return f.siftNoLock(nextLevel, args[1:], levelNum+1)
}

// NewFilter returns an empty filter to start loading processes into
func NewFilter(maxExactPathMatches int, fanOut []int) Filter {
	return &filterImpl{
		maxExactPathMatches: maxExactPathMatches,
		maxFanOut:           fanOut,

		containersInDeployment: make(map[string]map[string]*level),
	}
}

func (f *filterImpl) getOrAddRootLevelNoLock(indicator *storage.ProcessIndicator) *level {
	containerMap := f.containersInDeployment[indicator.GetDeploymentId()]
	if containerMap == nil {
		containerMap = make(map[string]*level)
		f.containersInDeployment[indicator.GetDeploymentId()] = containerMap
	}

	rootLevel := containerMap[indicator.GetSignal().GetContainerId()]
	if rootLevel == nil {
		rootLevel = newLevel()
		containerMap[indicator.GetSignal().GetContainerId()] = rootLevel
	}

	return rootLevel
}

func (f *filterImpl) Add(indicator *storage.ProcessIndicator) bool {
	f.rootLock.Lock()
	defer f.rootLock.Unlock()

	rootLevel := f.getOrAddRootLevelNoLock(indicator)

	// Handle the process level independently as we will never reject a new process
	processLevel := rootLevel.children[indicator.GetSignal().GetExecFilePath()]
	if processLevel == nil {
		processLevel = newLevel()
		rootLevel.children[indicator.GetSignal().GetExecFilePath()] = processLevel
	}

	return f.siftNoLock(processLevel, strings.Fields(indicator.GetSignal().GetArgs()), 0)
}

// Deprecated: When Pod/Deployment separation becomes enabled by default,
// the process filter will be updated on a per-pod basis.
func (f *filterImpl) Update(deployment *storage.Deployment) {
	f.rootLock.Lock()
	defer f.rootLock.Unlock()

	liveContainerSet := set.NewStringSet()
	for _, c := range deployment.GetContainers() {
		for _, inst := range c.GetInstances() {
			liveContainerSet.Add(inst.InstanceId.Id)
		}
	}

	containersMap := f.containersInDeployment[deployment.GetId()]
	for k := range containersMap {
		if !liveContainerSet.Contains(k) {
			delete(containersMap, k)
		}
	}
}

// TODO: Once Update(*storage.Deployment) is removed, consider renaming to just Update.
func (f *filterImpl) UpdateByGivenContainers(deploymentID string, liveContainerSet set.StringSet) {
	f.rootLock.Lock()
	defer f.rootLock.Unlock()

	containersMap := f.containersInDeployment[deploymentID]
	for k := range containersMap {
		if !liveContainerSet.Contains(k) {
			delete(containersMap, k)
		}
	}
}

func (f *filterImpl) Delete(deploymentID string) {
	f.rootLock.Lock()
	defer f.rootLock.Unlock()

	delete(f.containersInDeployment, deploymentID)
}
