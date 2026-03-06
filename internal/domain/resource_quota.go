package domain

import (
	"context"

	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
)

var (
	SupportedResourceQuotas = []string{"mem", "cpu", "disk"}
)

type ResourceQuotas map[string]float64

type ResourceQuotaStore interface {
	// todo: remove tx from the interface
	SetResourceQuotas(ctx context.Context, entityId string, quotas ResourceQuotas, tx neo4j.Transaction) error
	GetAvailableResources(ctx context.Context, tx neo4j.Transaction, entityId string) (ResourceQuotas, error)
}
