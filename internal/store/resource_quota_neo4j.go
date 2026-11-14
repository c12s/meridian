package store

import (
	"fmt"
	"log"

	"github.com/c12s/meridian/internal/domain"
	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
)

type resourceQuotaNeo4jStore struct {
	driver neo4j.Driver
	dbName string
}

func NewResourceQuotaNeo4jStore(driver neo4j.Driver, dbName string) domain.ResourceQuotaStore {
	if driver == nil {
		log.Fatalln("driver while initializing app neo4j store")
	}
	return &resourceQuotaNeo4jStore{
		driver: driver,
		dbName: dbName,
	}
}

func (n *resourceQuotaNeo4jStore) SetResourceQuotas(entityId string, quotas domain.ResourceQuotas, tx neo4j.Transaction) error {
	if tx == nil {
		session := startSession(n.driver, n.dbName)
		defer endSession(session)
		newTx, err := session.BeginTransaction()
		if err != nil {
			return err
		}
		tx = newTx
	}

	total, err := n.GetQuotas(tx, entityId)
	if err != nil {
		tx.Rollback()
		return err
	}

	parentEntityId, err := n.getParentEntity(tx, entityId)
	if err != nil {
		log.Println(err)
	} else {
		availableParent, err := n.GetAvailableResources(tx, parentEntityId)
		if err != nil {
			tx.Rollback()
			return err
		}
		for resource, quota := range quotas {
			if available := availableParent[resource] + total[resource]; available < quota {
				return fmt.Errorf("requested %f quota for the resource %s, but only %f available in parent", quota, resource, available)
			}
		}
	}

	available, err := n.GetAvailableResources(tx, entityId)
	if err != nil {
		tx.Rollback()
		return err
	}
	for resource, quota := range quotas {
		utilized := total[resource] - available[resource]
		if utilized > quota {
			return fmt.Errorf("requested %f quota for the resource %s, but %f already utilized", quota, resource, utilized)
		}
	}

	err = n.setResourceQuotas(tx, entityId, quotas)
	if err != nil {
		tx.Rollback()
		return err
	}
	tx.Commit()
	return nil
}

func (n *resourceQuotaNeo4jStore) GetQuotas(tx neo4j.Transaction, id string) (domain.ResourceQuotas, error) {
	if tx == nil {
		session := startSession(n.driver, n.dbName)
		defer endSession(session)
		newTx, err := session.BeginTransaction()
		if err != nil {
			return nil, err
		}
		tx = newTx
	}
	res, err := tx.Run(getEntityCypher, map[string]any{
		"id": id,
	})
	if err != nil {
		return nil, err
	}
	return n.readQuotas(res)
}

func (n *resourceQuotaNeo4jStore) getParentEntity(tx neo4j.Transaction, entityId string) (string, error) {
	res, err := tx.Run(getParentEntityCypher, map[string]any{
		"id": entityId,
	})
	if err != nil {
		return "", err
	}
	return n.readEntityId(res)
}

func (n *resourceQuotaNeo4jStore) GetAvailableResources(tx neo4j.Transaction, entityId string) (domain.ResourceQuotas, error) {
	if tx == nil {
		session := startSession(n.driver, n.dbName)
		defer endSession(session)
		newTx, err := session.BeginTransaction()
		if err != nil {
			return nil, err
		}
		tx = newTx
	}

	quotas := make(map[string]float64)
	for _, resourceName := range domain.SupportedResourceQuotas {
		res, err := tx.Run(getAvailableResourcesCypher, map[string]any{
			"id":            entityId,
			"resource_name": resourceName,
		})
		if err != nil {
			return nil, err
		}
		if res.Err() != nil {
			return nil, res.Err()
		}
		records, err := res.Collect()
		if err != nil {
			return nil, err
		}
		if len(records) == 0 || len(records[0].Values) == 0 {
			return nil, fmt.Errorf("available resources not found for resource %s", resourceName)
		}
		availableAny := records[0].Values[0]
		available, ok := availableAny.(float64)
		if !ok {
			log.Printf("available resources for %s cannot be converted to float: %v", resourceName, availableAny)
		} else {
			quotas[resourceName] = available
		}
	}
	return quotas, nil
}

func (n *resourceQuotaNeo4jStore) setResourceQuotas(tx neo4j.Transaction, entityId string, quotas domain.ResourceQuotas) error {
	for resource, quota := range quotas {
		_, err := tx.Run(setQuotaCypher(resource), map[string]any{
			"id":    entityId,
			"quota": quota,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *resourceQuotaNeo4jStore) readEntityId(res neo4j.Result) (string, error) {
	if res.Err() != nil {
		return "", res.Err()
	}
	records, err := res.Collect()
	if err != nil {
		return "", err
	}
	if len(records) == 0 {
		return "", fmt.Errorf("entity not found")
	}
	record := records[0]
	propertiesAny, found := record.Get("properties")
	if !found {
		return "", fmt.Errorf("entity has no properties")
	}
	properties, ok := propertiesAny.(map[string]any)
	if !ok {
		return "", fmt.Errorf("entity has no properties")
	}
	idAny, found := properties["id"]
	if !found {
		return "", fmt.Errorf("id not found")
	}
	id, ok := idAny.(string)
	if !ok {
		return "", fmt.Errorf("invalid id type: %v", idAny)
	}
	return id, nil
}

func (n *resourceQuotaNeo4jStore) readQuotas(res neo4j.Result) (domain.ResourceQuotas, error) {
	if res.Err() != nil {
		return nil, res.Err()
	}
	records, err := res.Collect()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("entity not found")
	}
	record := records[0]
	propertiesAny, found := record.Get("properties")
	if !found {
		return nil, fmt.Errorf("entity has no properties")
	}
	properties, ok := propertiesAny.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("entity has no properties")
	}
	quotas := make(domain.ResourceQuotas)
	for _, resourceName := range domain.SupportedResourceQuotas {
		quotaAny, found := properties[resourceName]
		if found {
			if quota, ok := quotaAny.(float64); !ok {
				log.Printf("invalid quota type for resource name %s: %v\n", resourceName, quotaAny)
			} else {
				quotas[resourceName] = quota
			}
		}
	}
	return quotas, nil
}

const getParentEntityCypher = `
MATCH (e:Entity{id: $id})
OPTIONAL MATCH (p:Entity)-[:CHILD]->(e)
RETURN properties(p) AS properties;
`

const getEntityCypher = `
MATCH (e:Entity{id: $id})
RETURN properties(e) AS properties;
`

const getAvailableResourcesCypher = `
MATCH (n:Entity{id: $id})
OPTIONAL MATCH (d:Entity)<-[:CHILD]-(n)
WITH n, collect(d[$resource_name]) as utilized_list
WITH reduce(total_utilized = 0, utilized in utilized_list | total_utilized + utilized) AS total_utilized,
	 n[$resource_name] AS total
RETURN total - total_utilized AS available;
`

func setQuotaCypher(resource string) string {
	return fmt.Sprintf("MATCH (e:Entity{id: $id})SET e.%s = $quota;", resource)
}
