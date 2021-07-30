package datastore

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	alertDataStore "github.com/stackrox/rox/central/alert/datastore"
	"github.com/stackrox/rox/central/cluster/datastore/internal/search"
	"github.com/stackrox/rox/central/cluster/index"
	"github.com/stackrox/rox/central/cluster/store"
	deploymentDataStore "github.com/stackrox/rox/central/deployment/datastore"
	namespaceDataStore "github.com/stackrox/rox/central/namespace/datastore"
	networkBaselineManager "github.com/stackrox/rox/central/networkbaseline/manager"
	netEntityDataStore "github.com/stackrox/rox/central/networkgraph/entity/datastore"
	netFlowDataStore "github.com/stackrox/rox/central/networkgraph/flow/datastore"
	nodeDataStore "github.com/stackrox/rox/central/node/globaldatastore"
	notifierProcessor "github.com/stackrox/rox/central/notifier/processor"
	podDataStore "github.com/stackrox/rox/central/pod/datastore"
	"github.com/stackrox/rox/central/ranking"
	"github.com/stackrox/rox/central/role/resources"
	secretDataStore "github.com/stackrox/rox/central/secret/datastore"
	"github.com/stackrox/rox/central/sensor/service/common"
	"github.com/stackrox/rox/central/sensor/service/connection"
	v1 "github.com/stackrox/rox/generated/api/v1"
	"github.com/stackrox/rox/generated/internalapi/central"
	"github.com/stackrox/rox/generated/storage"
	clusterValidation "github.com/stackrox/rox/pkg/cluster"
	"github.com/stackrox/rox/pkg/concurrency"
	"github.com/stackrox/rox/pkg/errorhelpers"
	"github.com/stackrox/rox/pkg/protoconv"
	"github.com/stackrox/rox/pkg/sac"
	pkgSearch "github.com/stackrox/rox/pkg/search"
	"github.com/stackrox/rox/pkg/set"
	"github.com/stackrox/rox/pkg/simplecache"
	"github.com/stackrox/rox/pkg/sync"
	"github.com/stackrox/rox/pkg/utils"
	"github.com/stackrox/rox/pkg/uuid"
)

const (
	connectionTerminationTimeout = 5 * time.Second

	// clusterMoveGracePeriod determines the amount of time that has to pass before a (logical) StackRox cluster can
	// be moved to a different (physical) Kubernetes cluster.
	clusterMoveGracePeriod = 3 * time.Minute
)

const (
	defaultAdmissionControllerTimeout = 3
)

var (
	clusterSAC = sac.ForResource(resources.Cluster)
)

type datastoreImpl struct {
	indexer              index.Indexer
	clusterStorage       store.ClusterStore
	clusterHealthStorage store.ClusterHealthStore
	notifier             notifierProcessor.Processor
	searcher             search.Searcher

	alertDataStore      alertDataStore.DataStore
	namespaceDataStore  namespaceDataStore.DataStore
	deploymentDataStore deploymentDataStore.DataStore
	nodeDataStore       nodeDataStore.GlobalDataStore
	podDataStore        podDataStore.DataStore
	secretsDataStore    secretDataStore.DataStore
	netFlowsDataStore   netFlowDataStore.ClusterDataStore
	netEntityDataStore  netEntityDataStore.EntityDataStore
	cm                  connection.Manager
	networkBaselineMgr  networkBaselineManager.Manager

	clusterRanker *ranking.Ranker

	idToNameCache simplecache.Cache
	nameToIDCache simplecache.Cache

	lock sync.Mutex
}

func (ds *datastoreImpl) UpdateClusterUpgradeStatus(ctx context.Context, id string, upgradeStatus *storage.ClusterUpgradeStatus) error {
	if err := checkWriteSac(ctx, id); err != nil {
		return err
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	cluster, err := ds.getClusterOnly(id)
	if err != nil {
		return err
	}

	if cluster.GetStatus() == nil {
		cluster.Status = &storage.ClusterStatus{}
	}

	cluster.Status.UpgradeStatus = upgradeStatus
	return ds.clusterStorage.Upsert(cluster)
}

func (ds *datastoreImpl) UpdateClusterCertExpiryStatus(ctx context.Context, id string, clusterCertExpiryStatus *storage.ClusterCertExpiryStatus) error {
	if err := checkWriteSac(ctx, id); err != nil {
		return err
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	cluster, err := ds.getClusterOnly(id)
	if err != nil {
		return err
	}

	if cluster.GetStatus() == nil {
		cluster.Status = &storage.ClusterStatus{}
	}

	cluster.Status.CertExpiryStatus = clusterCertExpiryStatus
	return ds.clusterStorage.Upsert(cluster)
}

func (ds *datastoreImpl) UpdateClusterStatus(ctx context.Context, id string, status *storage.ClusterStatus) error {
	if err := checkWriteSac(ctx, id); err != nil {
		return err
	}

	cluster, err := ds.getClusterOnly(id)
	if err != nil {
		return err
	}

	status.UpgradeStatus = cluster.GetStatus().GetUpgradeStatus()
	status.CertExpiryStatus = cluster.GetStatus().GetCertExpiryStatus()
	cluster.Status = status

	return ds.clusterStorage.Upsert(cluster)
}

func (ds *datastoreImpl) buildIndex() error {
	var clusters []*storage.Cluster
	err := ds.clusterStorage.Walk(func(cluster *storage.Cluster) error {
		clusters = append(clusters, cluster)
		return nil
	})
	if err != nil {
		return err
	}

	clusterHealthStatuses := make(map[string]*storage.ClusterHealthStatus)
	err = ds.clusterHealthStorage.WalkAllWithID(func(id string, healthInfo *storage.ClusterHealthStatus) error {
		clusterHealthStatuses[id] = healthInfo
		return nil
	})
	if err != nil {
		return err
	}

	for _, c := range clusters {
		ds.idToNameCache.Add(c.GetId(), c.GetName())
		ds.nameToIDCache.Add(c.GetName(), c.GetId())
		c.HealthStatus = clusterHealthStatuses[c.GetId()]
	}
	return ds.indexer.AddClusters(clusters)
}

func (ds *datastoreImpl) registerClusterForNetworkGraphExtSrcs() error {
	var clusters []*storage.Cluster
	if err := ds.clusterStorage.Walk(func(cluster *storage.Cluster) error {
		clusters = append(clusters, cluster)
		return nil
	}); err != nil {
		return err
	}

	ctx := sac.WithGlobalAccessScopeChecker(context.Background(),
		sac.AllowFixedScopes(
			sac.AccessModeScopeKeys(storage.Access_READ_ACCESS, storage.Access_READ_WRITE_ACCESS),
			sac.ResourceScopeKeys(resources.Node, resources.NetworkGraph)))

	for _, cluster := range clusters {
		ds.netEntityDataStore.RegisterCluster(ctx, cluster.GetId())
	}
	return nil
}

func (ds *datastoreImpl) Search(ctx context.Context, q *v1.Query) ([]pkgSearch.Result, error) {
	return ds.searcher.Search(ctx, q)
}

// Count returns the number of search results from the query
func (ds *datastoreImpl) Count(ctx context.Context, q *v1.Query) (int, error) {
	return ds.searcher.Count(ctx, q)
}

func (ds *datastoreImpl) SearchResults(ctx context.Context, q *v1.Query) ([]*v1.SearchResult, error) {
	return ds.searcher.SearchResults(ctx, q)
}

func (ds *datastoreImpl) searchRawClusters(ctx context.Context, q *v1.Query) ([]*storage.Cluster, error) {
	clusters, err := ds.searcher.SearchClusters(ctx, q)
	if err != nil {
		return nil, err
	}

	ds.populateHealthInfos(clusters...)
	ds.updateClusterPriority(clusters...)
	return clusters, nil
}

func (ds *datastoreImpl) GetCluster(ctx context.Context, id string) (*storage.Cluster, bool, error) {
	cluster, found, err := ds.clusterStorage.Get(id)
	if err != nil || !found {
		return nil, false, err
	}
	if ok, err := clusterSAC.ReadAllowed(ctx, sac.ClusterScopeKey(id)); err != nil || !ok {
		return nil, false, err
	}

	ds.populateHealthInfos(cluster)
	ds.updateClusterPriority(cluster)
	return cluster, true, nil
}

func (ds *datastoreImpl) GetClusters(ctx context.Context) ([]*storage.Cluster, error) {
	ok, err := clusterSAC.ReadAllowed(ctx)
	if err != nil {
		return nil, err
	} else if ok {
		var clusters []*storage.Cluster
		err := ds.clusterStorage.Walk(func(cluster *storage.Cluster) error {
			clusters = append(clusters, cluster)
			return nil
		})
		if err != nil {
			return nil, err
		}

		ds.populateHealthInfos(clusters...)
		ds.updateClusterPriority(clusters...)
		return clusters, nil
	}

	return ds.searchRawClusters(ctx, pkgSearch.EmptyQuery())
}

func (ds *datastoreImpl) GetClusterName(ctx context.Context, id string) (string, bool, error) {
	if ok, err := clusterSAC.ReadAllowed(ctx, sac.ClusterScopeKey(id)); err != nil || !ok {
		return "", false, err
	}
	val, ok := ds.idToNameCache.Get(id)
	if !ok {
		return "", false, nil
	}
	return val.(string), true, nil
}

func (ds *datastoreImpl) Exists(ctx context.Context, id string) (bool, error) {
	if ok, err := clusterSAC.ReadAllowed(ctx, sac.ClusterScopeKey(id)); err != nil || !ok {
		return false, err
	}
	_, ok := ds.idToNameCache.Get(id)
	return ok, nil
}

func (ds *datastoreImpl) SearchRawClusters(ctx context.Context, q *v1.Query) ([]*storage.Cluster, error) {
	clusters, err := ds.searchRawClusters(ctx, q)
	if err != nil {
		return nil, err
	}
	return clusters, nil
}

func (ds *datastoreImpl) CountClusters(ctx context.Context) (int, error) {
	if ok, err := clusterSAC.ReadAllowed(ctx); err != nil {
		return 0, err
	} else if ok {
		return ds.clusterStorage.Count()
	}

	return ds.Count(ctx, pkgSearch.EmptyQuery())
}

func checkWriteSac(ctx context.Context, clusterID string) error {
	if ok, err := clusterSAC.WriteAllowed(ctx, sac.ClusterScopeKey(clusterID)); err != nil {
		return err
	} else if !ok {
		return sac.ErrResourceAccessDenied
	}
	return nil
}

func (ds *datastoreImpl) AddCluster(ctx context.Context, cluster *storage.Cluster) (string, error) {
	if err := checkWriteSac(ctx, cluster.GetId()); err != nil {
		return "", err
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	return ds.addClusterNoLock(cluster)
}

func (ds *datastoreImpl) addClusterNoLock(cluster *storage.Cluster) (string, error) {
	if cluster.GetId() != "" {
		return "", errors.Errorf("cannot add a cluster that has already been assigned an id: %q", cluster.GetId())
	}

	if cluster.GetName() == "" {
		return "", errors.New("cannot add a cluster without name")
	}

	cluster.Id = uuid.NewV4().String()
	if err := ds.updateClusterNoLock(cluster); err != nil {
		return "", err
	}

	// Temporarily elevate permissions to create network flow store for the cluster.
	networkGraphElevatedCtx := sac.WithGlobalAccessScopeChecker(context.Background(),
		sac.AllowFixedScopes(
			sac.AccessModeScopeKeys(storage.Access_READ_WRITE_ACCESS),
			sac.ResourceScopeKeys(resources.NetworkGraph)))

	if _, err := ds.netFlowsDataStore.CreateFlowStore(networkGraphElevatedCtx, cluster.GetId()); err != nil {
		return "", errors.Wrapf(err, "could not create flow store for cluster %s", cluster.GetId())
	}
	return cluster.GetId(), nil
}

func (ds *datastoreImpl) UpdateCluster(ctx context.Context, cluster *storage.Cluster) error {
	if err := checkWriteSac(ctx, cluster.GetId()); err != nil {
		return err
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	existingCluster, exists, err := ds.clusterStorage.Get(cluster.GetId())
	if err != nil {
		return err
	}
	if exists {
		if cluster.GetName() != existingCluster.GetName() {
			return errors.Errorf("cannot update cluster. Cluster name change from %s not permitted", existingCluster.GetName())
		}
		cluster.Status = existingCluster.GetStatus()
	}

	if err := ds.updateClusterNoLock(cluster); err != nil {
		return err
	}

	conn := ds.cm.GetConnection(cluster.GetId())
	if conn == nil {
		return nil
	}
	err = conn.InjectMessage(concurrency.Never(), &central.MsgToSensor{
		Msg: &central.MsgToSensor_ClusterConfig{
			ClusterConfig: &central.ClusterConfig{
				Config: cluster.GetDynamicConfig(),
			},
		},
	})
	if err != nil {
		// This is just logged because the connection could have been broken during the config send and we should handle it gracefully
		log.Error(err)
	}
	return nil
}

func (ds *datastoreImpl) UpdateClusterHealth(ctx context.Context, id string, clusterHealthStatus *storage.ClusterHealthStatus) error {
	if id == "" {
		return errors.New("cannot update cluster health. cluster id not provided")
	}

	if clusterHealthStatus == nil {
		return errors.Errorf("cannot update health for cluster %s. No health information available", id)
	}

	if err := checkWriteSac(ctx, id); err != nil {
		return err
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	oldHealth, _, err := ds.clusterHealthStorage.Get(id)
	if err != nil {
		return err
	}

	if err := ds.clusterHealthStorage.UpsertWithID(id, clusterHealthStatus); err != nil {
		return err
	}

	// If no change in cluster health status, no need to rebuild index
	if clusterHealthStatus.GetSensorHealthStatus() == oldHealth.GetSensorHealthStatus() && clusterHealthStatus.GetCollectorHealthStatus() == oldHealth.GetCollectorHealthStatus() {
		return nil
	}

	cluster, exists, err := ds.clusterStorage.Get(id)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	cluster.HealthStatus = clusterHealthStatus
	return ds.indexer.AddCluster(cluster)
}

func (ds *datastoreImpl) UpdateSensorDeploymentIdentification(ctx context.Context, id string, identification *storage.SensorDeploymentIdentification) error {
	if err := checkWriteSac(ctx, id); err != nil {
		return err
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	cluster, err := ds.getClusterOnly(id)
	if err != nil {
		return err
	}

	cluster.MostRecentSensorId = identification
	return ds.clusterStorage.Upsert(cluster)
}

func (ds *datastoreImpl) UpdateAuditLogFileStates(ctx context.Context, id string, states map[string]*storage.AuditLogFileState) error {
	if id == "" {
		return errors.New("cannot update audit log file states because cluster id was not provided")
	}
	if len(states) == 0 {
		return errors.Errorf("cannot update audit log file states for cluster %s. No state information available", id)
	}

	if err := checkWriteSac(ctx, id); err != nil {
		return err
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	cluster, err := ds.getClusterOnly(id)
	if err != nil {
		return err
	}

	// If a state is missing in the new update, keep it in the saved state.
	// It could be that compliance is down temporarily and we don't want to lose the data
	if cluster.GetAuditLogState() == nil {
		cluster.AuditLogState = make(map[string]*storage.AuditLogFileState)
	}
	for node, state := range states {
		cluster.AuditLogState[node] = state
	}

	return ds.clusterStorage.Upsert(cluster)
}

func (ds *datastoreImpl) RemoveCluster(ctx context.Context, id string, done *concurrency.Signal) error {
	if err := checkWriteSac(ctx, id); err != nil {
		return err
	}

	ds.lock.Lock()
	defer ds.lock.Unlock()

	// Fetch the cluster an confirm it exists.
	cluster, exists, err := ds.clusterStorage.Get(id)
	if !exists {
		return errors.Errorf("unable to find cluster %q", id)
	}
	if err != nil {
		return err
	}

	if err := ds.clusterStorage.Delete(id); err != nil {
		return errors.Wrapf(err, "failed to remove cluster %q", id)
	}
	ds.idToNameCache.Remove(id)
	ds.nameToIDCache.Remove(cluster.GetName())

	deleteRelatedCtx := sac.WithAllAccess(context.Background())
	go ds.postRemoveCluster(deleteRelatedCtx, cluster, done)
	return ds.indexer.DeleteCluster(id)
}

func (ds *datastoreImpl) postRemoveCluster(ctx context.Context, cluster *storage.Cluster, done *concurrency.Signal) {
	// Terminate the cluster connection to prevent new data from being stored.
	if ds.cm != nil {
		if conn := ds.cm.GetConnection(cluster.GetId()); conn != nil {
			conn.Terminate(errors.New("cluster was deleted"))
			if !concurrency.WaitWithTimeout(conn.Stopped(), connectionTerminationTimeout) {
				utils.Should(errors.Errorf("connection to sensor from cluster %s not terminated after %v", cluster.GetId(), connectionTerminationTimeout))
			}
		}
	}

	// Remove ranker record here since removal is not handled in risk store as no entry present for cluster
	ds.clusterRanker.Remove(cluster.GetId())

	ds.removeClusterNamespaces(ctx, cluster)

	// Tombstone each deployment and mark alerts stale.
	ds.removeClusterDeployments(ctx, cluster)

	ds.removeClusterPods(ctx, cluster)

	// Remove nodes associated with this cluster
	if err := ds.nodeDataStore.RemoveClusterNodeStores(ctx, cluster.GetId()); err != nil {
		log.Errorf("failed to remove nodes for cluster %s: %v", cluster.GetId(), err)
	}

	if err := ds.netEntityDataStore.DeleteExternalNetworkEntitiesForCluster(ctx, cluster.GetId()); err != nil {
		log.Errorf("failed to delete external network graph entities for removed cluster %s: %v", cluster.GetId(), err)
	}

	err := ds.networkBaselineMgr.ProcessPostClusterDelete(cluster.GetId())
	if err != nil {
		log.Errorf("failed to delete network baselines associated with this cluster %q: %v", cluster.GetId(), err)
	}

	ds.removeClusterSecrets(ctx, cluster)
	if done != nil {
		done.Signal()
	}
}

func (ds *datastoreImpl) removeClusterNamespaces(ctx context.Context, cluster *storage.Cluster) {
	q := pkgSearch.NewQueryBuilder().AddExactMatches(pkgSearch.ClusterID, cluster.GetId()).ProtoQuery()
	namespaces, err := ds.namespaceDataStore.Search(ctx, q)
	if err != nil {
		log.Errorf("failed to get namespaces for removed cluster %s: %v", cluster.GetId(), err)
	}

	for _, namespace := range namespaces {
		err = ds.namespaceDataStore.RemoveNamespace(ctx, namespace.ID)
		if err != nil {
			log.Errorf("failed to remove namespace %s in deleted cluster: %v", namespace.ID, err)
		}
	}

}

func (ds *datastoreImpl) removeClusterPods(ctx context.Context, cluster *storage.Cluster) {
	q := pkgSearch.NewQueryBuilder().AddExactMatches(pkgSearch.ClusterID, cluster.GetId()).ProtoQuery()
	pods, err := ds.podDataStore.Search(ctx, q)
	if err != nil {
		log.Errorf("Failed to get pods for removed cluster %s: %v", cluster.GetId(), err)
		return
	}
	for _, pod := range pods {
		if err := ds.podDataStore.RemovePod(ctx, pod.ID); err != nil {
			log.Errorf("Failed to remove pod with id %s as part of removal of cluster %s: %v", pod.ID, cluster.GetId(), err)
		}
	}
}

func (ds *datastoreImpl) removeClusterDeployments(ctx context.Context, cluster *storage.Cluster) {
	q := pkgSearch.NewQueryBuilder().AddExactMatches(pkgSearch.ClusterID, cluster.GetId()).ProtoQuery()
	deployments, err := ds.deploymentDataStore.Search(ctx, q)
	if err != nil {
		log.Errorf("failed to get deployments for removed cluster %s: %v", cluster.GetId(), err)
	}
	// Tombstone each deployment and mark alerts stale.
	for _, deployment := range deployments {
		alerts, err := ds.getAlerts(ctx, deployment.ID)
		if err != nil {
			log.Errorf("failed to retrieve alerts for deployment %s: %v", deployment.ID, err)
		} else {
			err = ds.markAlertsStale(ctx, alerts)
			if err != nil {
				log.Errorf("failed to mark alerts for deployment %s as stale: %v", deployment.ID, err)
			}
		}

		err = ds.deploymentDataStore.RemoveDeployment(ctx, cluster.GetId(), deployment.ID)
		if err != nil {
			log.Errorf("failed to remove deployment %s in deleted cluster: %v", deployment.ID, err)
		}
	}

}

func (ds *datastoreImpl) removeClusterSecrets(ctx context.Context, cluster *storage.Cluster) {
	secrets, err := ds.getSecrets(ctx, cluster)
	if err != nil {
		log.Errorf("failed to obtain secrets in deleted cluster %s: %v", cluster.GetId(), err)
	}
	for _, s := range secrets {
		// Best effort to remove. If the object doesn't exist, then that is okay
		_ = ds.secretsDataStore.RemoveSecret(ctx, s.GetId())
	}
}

func (ds *datastoreImpl) getSecrets(ctx context.Context, cluster *storage.Cluster) ([]*storage.ListSecret, error) {
	q := pkgSearch.NewQueryBuilder().AddExactMatches(pkgSearch.ClusterID, cluster.GetId()).ProtoQuery()
	return ds.secretsDataStore.SearchListSecrets(ctx, q)
}

func (ds *datastoreImpl) getAlerts(ctx context.Context, deploymentID string) ([]*storage.Alert, error) {
	q := pkgSearch.NewQueryBuilder().
		AddStrings(pkgSearch.ViolationState, storage.ViolationState_ACTIVE.String()).
		AddExactMatches(pkgSearch.DeploymentID, deploymentID).ProtoQuery()
	return ds.alertDataStore.SearchRawAlerts(ctx, q)
}

func (ds *datastoreImpl) markAlertsStale(ctx context.Context, alerts []*storage.Alert) error {
	errorList := errorhelpers.NewErrorList("unable to mark some alerts stale")
	for _, alert := range alerts {
		errorList.AddError(ds.alertDataStore.MarkAlertStale(ctx, alert.GetId()))
		if errorList.ToError() == nil {
			// run notifier for all the resolved alerts
			alert.State = storage.ViolationState_RESOLVED
			ds.notifier.ProcessAlert(ctx, alert)
		}
	}
	return errorList.ToError()
}

func (ds *datastoreImpl) cleanUpNodeStore(ctx context.Context) {
	if err := ds.doCleanUpNodeStore(ctx); err != nil {
		log.Errorf("Error cleaning up cluster node stores: %v", err)
	}
}

func (ds *datastoreImpl) doCleanUpNodeStore(ctx context.Context) error {
	clusterNodeStores, err := ds.nodeDataStore.GetAllClusterNodeStores(ctx, false)
	if err != nil {
		return errors.Wrap(err, "retrieving per-cluster node stores")
	}

	if len(clusterNodeStores) == 0 {
		return nil
	}

	orphanedClusterIDsInNodeStore := set.NewStringSet()
	for clusterID := range clusterNodeStores {
		orphanedClusterIDsInNodeStore.Add(clusterID)
	}

	clusters, err := ds.GetClusters(ctx)
	if err != nil {
		return errors.Wrap(err, "retrieving clusters")
	}
	for _, cluster := range clusters {
		orphanedClusterIDsInNodeStore.Remove(cluster.GetId())
	}

	return ds.nodeDataStore.RemoveClusterNodeStores(ctx, orphanedClusterIDsInNodeStore.AsSlice()...)
}

func (ds *datastoreImpl) updateClusterPriority(clusters ...*storage.Cluster) {
	for _, cluster := range clusters {
		cluster.Priority = ds.clusterRanker.GetRankForID(cluster.GetId())
	}
}

func (ds *datastoreImpl) getClusterOnly(id string) (*storage.Cluster, error) {
	cluster, exists, err := ds.clusterStorage.Get(id)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.Errorf("cluster %s not found", id)
	}
	return cluster, nil
}

func (ds *datastoreImpl) populateHealthInfos(clusters ...*storage.Cluster) {
	ids := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		ids = append(ids, cluster.GetId())
	}

	infos, missing, err := ds.clusterHealthStorage.GetMany(ids)
	if err != nil {
		log.Errorf("failed to populate health info for %d clusters: %v", len(ids), err)
		return
	}
	if len(infos) == 0 {
		return
	}

	missCount := 0
	healthIdx := 0
	for clusterIdx, cluster := range clusters {
		if missCount < len(missing) && clusterIdx == missing[missCount] {
			missCount++
			continue
		}
		cluster.HealthStatus = infos[healthIdx]
		healthIdx++
	}
}

func (ds *datastoreImpl) updateClusterNoLock(cluster *storage.Cluster) error {
	if err := normalizeCluster(cluster); err != nil {
		return err
	}
	if err := validateInput(cluster); err != nil {
		return err
	}

	if err := ds.clusterStorage.Upsert(cluster); err != nil {
		return err
	}
	if err := ds.indexer.AddCluster(cluster); err != nil {
		return err
	}
	ds.idToNameCache.Add(cluster.GetId(), cluster.GetName())
	ds.nameToIDCache.Add(cluster.GetName(), cluster.GetId())
	return nil
}

func (ds *datastoreImpl) LookupOrCreateClusterFromConfig(ctx context.Context, clusterID string, hello *central.SensorHello) (*storage.Cluster, error) {
	if err := checkWriteSac(ctx, clusterID); err != nil {
		return nil, err
	}

	helmConfig := hello.GetHelmManagedConfigInit()

	ds.lock.Lock()
	defer ds.lock.Unlock()

	clusterName := helmConfig.GetClusterName()

	if clusterID == "" && clusterName != "" {
		// Try to look up cluster ID by name, if this is for an existing cluster
		clusterIDVal, _ := ds.nameToIDCache.Get(clusterName)
		clusterID, _ = clusterIDVal.(string)
	}

	isExisting := false
	var cluster *storage.Cluster
	if clusterID != "" {
		clusterByID, exist, err := ds.GetCluster(ctx, clusterID)
		if err != nil {
			return nil, err
		} else if !exist {
			return nil, errors.Errorf("cluster with ID %q does not exist", clusterID)
		}

		isExisting = true

		// If a name is specified, validate it (otherwise, accept any name)
		if clusterName != "" && clusterName != clusterByID.GetName() {
			return nil, errors.Errorf("Name mismatch for cluster %q: expected %q, but %q was specified. Set the cluster.name/clusterName attribute in your Helm config to %q, or remove it", clusterID, cluster.GetName(), clusterName, cluster.GetName())
		}

		cluster = clusterByID
	} else if clusterName != "" {
		// A this point, we can be sure that the cluster does not exist.
		cluster = &storage.Cluster{
			Name:               clusterName,
			MostRecentSensorId: hello.GetDeploymentIdentification().Clone(),
		}
		clusterConfig := helmConfig.GetClusterConfig()
		configureFromHelmConfig(cluster, clusterConfig)
		if !helmConfig.GetNotHelmManaged() {
			cluster.HelmConfig = clusterConfig.Clone()
		}
		if _, err := ds.addClusterNoLock(cluster); err != nil {
			return nil, errors.Wrapf(err, "failed to dynamically add cluster with name %q", clusterName)
		}
	} else {
		return nil, errors.New("neither a cluster ID nor a cluster name was specified")
	}

	// If the cluster should not be helm-managed, clear out the helm config field, if necessary.
	if helmConfig == nil || helmConfig.GetNotHelmManaged() {
		if cluster.GetHelmConfig() != nil {
			cluster.HelmConfig = nil
			if err := ds.updateClusterNoLock(cluster); err != nil {
				return nil, err
			}
		}
		return cluster, nil
	}

	if isExisting {
		// Check if the newly incoming request may replace the old connection
		lastContact := protoconv.ConvertTimestampToTimeOrDefault(cluster.GetHealthStatus().GetLastContact(), time.Time{})
		timeLeftInGracePeriod := clusterMoveGracePeriod - time.Since(lastContact)

		if timeLeftInGracePeriod > 0 {
			if err := common.CheckConnReplace(hello.GetDeploymentIdentification(), cluster.GetMostRecentSensorId()); err != nil {
				return nil, errors.Errorf("registering Helm-managed cluster is not allowed: %s. If you recently re-deployed, please wait for another %v", err, timeLeftInGracePeriod)
			}
		}

		if cluster.GetHelmConfig().GetConfigFingerprint() == helmConfig.GetClusterConfig().GetConfigFingerprint() {
			// No change in fingerprint, do not update. Note: this also is the case if the cluster was newly added.
			return cluster, nil
		}
	}

	// We know that the cluster is helm-managed at this point
	clusterConfig := helmConfig.GetClusterConfig()
	configureFromHelmConfig(cluster, clusterConfig)
	cluster.HelmConfig = clusterConfig.Clone()

	if err := ds.updateClusterNoLock(cluster); err != nil {
		return nil, err
	}

	return cluster, nil
}

func normalizeCluster(cluster *storage.Cluster) error {
	cluster.CentralApiEndpoint = strings.TrimPrefix(cluster.GetCentralApiEndpoint(), "https://")
	cluster.CentralApiEndpoint = strings.TrimPrefix(cluster.GetCentralApiEndpoint(), "http://")

	return addDefaults(cluster)
}

func validateInput(cluster *storage.Cluster) error {
	return clusterValidation.Validate(cluster).ToError()
}

func addDefaults(cluster *storage.Cluster) error {
	// For backwards compatibility reasons, if Collection Method is not set then honor defaults for runtime support
	if cluster.GetCollectionMethod() == storage.CollectionMethod_UNSET_COLLECTION {
		cluster.CollectionMethod = storage.CollectionMethod_KERNEL_MODULE
	}
	cluster.RuntimeSupport = cluster.GetCollectionMethod() != storage.CollectionMethod_NO_COLLECTION

	if cluster.GetTolerationsConfig() == nil {
		cluster.TolerationsConfig = &storage.TolerationsConfig{
			Disabled: false,
		}
	}

	if cluster.GetDynamicConfig() == nil {
		cluster.DynamicConfig = &storage.DynamicClusterConfig{}
	}
	if cluster.GetType() != storage.ClusterType_OPENSHIFT4_CLUSTER {
		cluster.DynamicConfig.DisableAuditLogs = true
	}

	acConfig := cluster.DynamicConfig.GetAdmissionControllerConfig()
	if acConfig == nil {
		acConfig = &storage.AdmissionControllerConfig{
			Enabled: false,
		}
		cluster.DynamicConfig.AdmissionControllerConfig = acConfig
	}
	if acConfig.GetTimeoutSeconds() < 0 {
		return fmt.Errorf("timeout of %d is invalid", acConfig.GetTimeoutSeconds())
	}
	if acConfig.GetTimeoutSeconds() == 0 {
		acConfig.TimeoutSeconds = defaultAdmissionControllerTimeout
	}
	return nil
}

func configureFromHelmConfig(cluster *storage.Cluster, helmConfig *storage.CompleteClusterConfig) {
	cluster.DynamicConfig = helmConfig.GetDynamicConfig().Clone()

	staticConfig := helmConfig.GetStaticConfig()
	cluster.Labels = helmConfig.GetClusterLabels()
	cluster.Type = staticConfig.GetType()
	cluster.MainImage = staticConfig.GetMainImage()
	cluster.CentralApiEndpoint = staticConfig.GetCentralApiEndpoint()
	cluster.CollectionMethod = staticConfig.GetCollectionMethod()
	cluster.CollectorImage = staticConfig.GetCollectorImage()
	cluster.AdmissionController = staticConfig.GetAdmissionController()
	cluster.AdmissionControllerUpdates = staticConfig.GetAdmissionControllerUpdates()
	cluster.AdmissionControllerEvents = staticConfig.GetAdmissionControllerEvents()
	cluster.TolerationsConfig = staticConfig.GetTolerationsConfig().Clone()
	cluster.SlimCollector = staticConfig.GetSlimCollector()
}
