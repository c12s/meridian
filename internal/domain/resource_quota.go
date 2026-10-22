package domain

import "github.com/neo4j/neo4j-go-driver/v4/neo4j"

var (
	SupportedResourceQuotas = []string{"mem", "cpu", "disk"}
)

type ResourceQuotas map[string]float64

type ResourceQuotaStore interface {
	// todo: remove tx from the interface
	SetResourceQuotas(entityId string, quotas ResourceQuotas, tx neo4j.Transaction) error
	GetAvailableResources(tx neo4j.Transaction, entityId string) (ResourceQuotas, error)
	GetQuotas(tx neo4j.Transaction, id string) (ResourceQuotas, error)
}
