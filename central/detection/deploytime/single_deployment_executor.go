package deploytime

import (
	"context"
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	deploymentIndexer "github.com/stackrox/rox/central/deployment/index"
	"github.com/stackrox/rox/central/detection"
	"github.com/stackrox/rox/central/globalindex"
	imageIndexer "github.com/stackrox/rox/central/image/index"
	"github.com/stackrox/rox/central/searchbasedpolicies"
	"github.com/stackrox/rox/generated/storage"
	"github.com/stackrox/rox/pkg/images/types"
	"github.com/stackrox/rox/pkg/search"
	"github.com/stackrox/rox/pkg/search/blevesearch"
	"github.com/stackrox/rox/pkg/utils"
)

func newSingleDeploymentExecutor(executorCtx context.Context, ctx DetectionContext, deployment *storage.Deployment, images []*storage.Image) alertCollectingExecutor {
	return &policyExecutor{
		executorCtx: executorCtx,
		ctx:         ctx,
		deployment:  deployment,
		images:      images,
	}
}

type closeableIndex interface {
	Close() error
}

type policyExecutor struct {
	executorCtx context.Context
	ctx         DetectionContext
	deployment  *storage.Deployment
	images      []*storage.Image
	alerts      []*storage.Alert
}

func (d *policyExecutor) GetAlerts() []*storage.Alert {
	return d.alerts
}

func (d *policyExecutor) ClearAlerts() {
	d.alerts = nil
}

func (d *policyExecutor) Execute(compiled detection.CompiledPolicy) error {
	if compiled.Policy().GetDisabled() {
		return nil
	}
	// Check predicate on deployment.
	if !compiled.AppliesTo(d.deployment) {
		return nil
	}

	// Check enforcement on deployment if we don't want unenforced alerts.
	enforcement, _ := buildEnforcement(compiled.Policy(), d.deployment)
	if enforcement == storage.EnforcementAction_UNSET_ENFORCEMENT && d.ctx.EnforcementOnly {
		return nil
	}

	// Generate violations.
	violations, err := d.getViolations(d.executorCtx, enforcement, compiled.Matcher())
	if err != nil {
		return errors.Wrapf(err, "evaluating violations for policy %s; deployment %s/%s", compiled.Policy().GetName(), d.deployment.GetNamespace(), d.deployment.GetName())
	}
	if len(violations) > 0 {
		d.alerts = append(d.alerts, policyDeploymentAndViolationsToAlert(compiled.Policy(), d.deployment, violations))
	}
	return nil
}

func (d *policyExecutor) getViolations(ctx context.Context, enforcement storage.EnforcementAction, matcher searchbasedpolicies.Matcher) ([]*storage.Alert_Violation, error) {
	closeableIndex, deploymentIndex, deploymentID, err := singleDeploymentSearcher(d.deployment, d.images)
	if err != nil {
		return nil, err
	}
	defer utils.IgnoreError(closeableIndex.Close)
	violations, err := matcher.MatchOne(ctx, deploymentIndex, deploymentID, nil)
	if err != nil {
		return nil, err
	}

	return violations.AlertViolations, nil
}

const deploymentID = "deployment-id"

func singleDeploymentSearcher(deployment *storage.Deployment, images []*storage.Image) (closeableIndex, search.Searcher, string, error) {
	clonedDeployment := proto.Clone(deployment).(*storage.Deployment)
	if clonedDeployment.GetId() == "" {
		clonedDeployment.Id = deploymentID
	}

	tempIndex, err := globalindex.MemOnlyIndex()
	if err != nil {
		return tempIndex, nil, "", errors.Wrap(err, "initializing temp index")
	}
	indexToClose := tempIndex
	defer func() {
		if indexToClose != nil {
			utils.IgnoreError(indexToClose.Close)
		}
	}()

	imageIndex := imageIndexer.New(tempIndex)
	deploymentIndex := deploymentIndexer.New(tempIndex)
	for i, img := range images {
		clonedImg := proto.Clone(img).(*storage.Image)
		if clonedImg.GetId() == "" {
			clonedImg.Id = fmt.Sprintf("image-id-%d", i)
		}
		if err := imageIndex.AddImage(clonedImg); err != nil {
			return nil, nil, "", err
		}
		if i >= len(clonedDeployment.GetContainers()) {
			log.Error("Found more images than containers")
		} else {
			clonedDeployment.Containers[i].Image = types.ToContainerImage(clonedImg)
		}
	}
	if err := deploymentIndex.AddDeployment(clonedDeployment); err != nil {
		return nil, nil, "", err
	}
	indexToClose = nil
	return tempIndex, blevesearch.WrapUnsafeSearcherAsSearcher(deploymentIndex), clonedDeployment.GetId(), nil
}
