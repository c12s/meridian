package domain

import "github.com/neo4j/neo4j-go-driver/v4/neo4j"

var (
	SupportedResourceQuotas = []string{"mem", "cpu", "disk"}
)

type ResourceQuotas map[string]float64

type ResourceQuotaStore interface {
	SetResourceQuotas(entityId string, quotas ResourceQuotas, tx neo4j.Transaction) error
}