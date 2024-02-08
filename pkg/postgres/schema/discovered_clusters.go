// Code generated by pg-bindings generator. DO NOT EDIT.

package schema

import (
	"fmt"
	"reflect"
	"time"

	v1 "github.com/stackrox/rox/generated/api/v1"
	"github.com/stackrox/rox/generated/storage"
	"github.com/stackrox/rox/pkg/postgres"
	"github.com/stackrox/rox/pkg/postgres/walker"
	"github.com/stackrox/rox/pkg/sac/resources"
	"github.com/stackrox/rox/pkg/search"
	"github.com/stackrox/rox/pkg/search/postgres/mapping"
)

var (
	// CreateTableDiscoveredClustersStmt holds the create statement for table `discovered_clusters`.
	CreateTableDiscoveredClustersStmt = &postgres.CreateStmts{
		GormModel: (*DiscoveredClusters)(nil),
		Children:  []*postgres.CreateStmts{},
	}

	// DiscoveredClustersSchema is the go schema for table `discovered_clusters`.
	DiscoveredClustersSchema = func() *walker.Schema {
		schema := GetSchemaForTable("discovered_clusters")
		if schema != nil {
			return schema
		}
		schema = walker.Walk(reflect.TypeOf((*storage.DiscoveredCluster)(nil)), "discovered_clusters")
		referencedSchemas := map[string]*walker.Schema{
			"storage.CloudSource": CloudSourcesSchema,
		}

		schema.ResolveReferences(func(messageTypeName string) *walker.Schema {
			return referencedSchemas[fmt.Sprintf("storage.%s", messageTypeName)]
		})
		schema.SetOptionsMap(search.Walk(v1.SearchCategory_DISCOVERED_CLUSTERS, "discoveredcluster", (*storage.DiscoveredCluster)(nil)))
		schema.ScopingResource = resources.Administration
		RegisterTable(schema, CreateTableDiscoveredClustersStmt)
		mapping.RegisterCategoryToTable(v1.SearchCategory_DISCOVERED_CLUSTERS, schema)
		return schema
	}()
)

const (
	// DiscoveredClustersTableName specifies the name of the table in postgres.
	DiscoveredClustersTableName = "discovered_clusters"
)

// DiscoveredClusters holds the Gorm model for Postgres table `discovered_clusters`.
type DiscoveredClusters struct {
	ID            string                           `gorm:"column:id;type:uuid;primaryKey"`
	MetadataName  string                           `gorm:"column:metadata_name;type:varchar"`
	MetadataType  storage.ClusterMetadata_Type     `gorm:"column:metadata_type;type:integer"`
	Status        storage.DiscoveredCluster_Status `gorm:"column:status;type:integer"`
	SourceID      string                           `gorm:"column:sourceid;type:uuid"`
	LastUpdatedAt *time.Time                       `gorm:"column:lastupdatedat;type:timestamp"`
	Serialized    []byte                           `gorm:"column:serialized;type:bytea"`
}
