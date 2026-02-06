package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/c12s/meridian/internal/domain"
	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
	"go.opentelemetry.io/otel"
)

type namespaceNeo4jStore struct {
	driver neo4j.Driver
	dbName string
	quotas domain.ResourceQuotaStore
	apps   domain.AppStore
}

func NewNamespaceNeo4jStore(driver neo4j.Driver, dbName string, quotas domain.ResourceQuotaStore, apps domain.AppStore) domain.NamespaceStore {
	if driver == nil {
		log.Fatalln("driver is nil while initializing namespace neo4j store")
	}
	return &namespaceNeo4jStore{
		driver: driver,
		dbName: dbName,
		quotas: quotas,
		apps:   apps,
	}
}

func (n *namespaceNeo4jStore) Add(ctx context.Context, namespace domain.Namespace, parent *domain.Namespace) error {

	tracer := otel.Tracer("meridian.NamespaceNeo4jStore")
	ctx, span := tracer.Start(ctx, "Add")
	defer span.End()

	session := startSession(n.driver, n.dbName)
	defer endSession(session)
	tx, err := session.BeginTransaction()
	if err != nil {
		return err
	}

	_, err = tx.Run(addNamespaceCypher, map[string]any{
		"id":              namespace.GetId(),
		"org_id":          namespace.GetOrgId(),
		"name":            namespace.GetName(),
		"profile_version": namespace.GetProfileVersion(),
		"labels":          namespace.GetLabelsJson(),
	})
	if err != nil {
		tx.Rollback()
		return err
	}

	if parent != nil {
		_, err = tx.Run(connectNamespacesCypher, map[string]any{
			"parent_id": parent.GetId(),
			"child_id":  namespace.GetId(),
		})
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	err = n.quotas.SetResourceQuotas(ctx, namespace.GetId(), namespace.GetResourceQuotas(), tx)
	if err != nil {
		tx.Rollback()
		return err
	}
	tx.Commit()
	return nil
}

func (n *namespaceNeo4jStore) Get(ctx context.Context, id string) (domain.Namespace, error) {
	tracer := otel.Tracer("meridian.NamespaceNeo4jStore")
	ctx, span := tracer.Start(ctx, "Get")
	defer span.End()

	session := startSession(n.driver, n.dbName)
	defer endSession(session)
	tx, err := session.BeginTransaction()
	if err != nil {
		return domain.Namespace{}, err
	}
	defer tx.Commit()
	namespace, err := n.get(ctx, tx, id)
	if err != nil {
		return domain.Namespace{}, err
	}
	return namespace, nil
}

func (n *namespaceNeo4jStore) GetHierarchy(ctx context.Context, rootId string) (domain.NamespaceTree, error) {
	tracer := otel.Tracer("meridian.NamespaceNeo4jStore")
	ctx, span := tracer.Start(ctx, "GetHierarchy")
	defer span.End()

	session := startSession(n.driver, n.dbName)
	defer endSession(session)
	tx, err := session.BeginTransaction()
	if err != nil {
		return domain.NamespaceTree{}, err
	}
	defer tx.Commit()
	root, err := n.get(ctx, tx, rootId)
	if err != nil {
		tx.Rollback()
		return domain.NamespaceTree{}, err
	}
	rootNode := &domain.NamespaceTreeNode{Namespace: &root}
	err = n.populateTree(ctx, tx, rootNode)
	if err != nil {
		tx.Rollback()
		return domain.NamespaceTree{}, err
	}
	return domain.NamespaceTree{Root: *rootNode}, nil
}

func (n *namespaceNeo4jStore) Remove(ctx context.Context, id string) error {
	tracer := otel.Tracer("meridian.NamespaceNeo4jStore")
	ctx, span := tracer.Start(ctx, "Remove")
	defer span.End()

	session := startSession(n.driver, n.dbName)
	defer endSession(session)
	tx, err := session.BeginTransaction()
	if err != nil {
		return err
	}
	_, err = tx.Run(removeNamespaceCypher, map[string]any{
		"id": id,
	})
	if err != nil {
		tx.Rollback()
		return err
	}
	tx.Commit()
	return nil
}

func (n *namespaceNeo4jStore) get(ctx context.Context, tx neo4j.Transaction, id string) (domain.Namespace, error) {
	res, err := tx.Run(getNamespaceCypher, map[string]any{
		"id": id,
	})
	if err != nil {
		return domain.Namespace{}, err
	}

	namespaces, err := n.readNamespaces(res, id)
	if err != nil {
		return domain.Namespace{}, err
	}
	if len(namespaces) == 0 {
		return domain.Namespace{}, fmt.Errorf("cannot find namespace %s", id)
	}
	available, err := n.quotas.GetAvailableResources(ctx, tx, namespaces[0].GetId())
	if err != nil {
		return domain.Namespace{}, err
	}
	err = namespaces[0].SetAvailable(available)
	if err != nil {
		return domain.Namespace{}, err
	}
	return namespaces[0], nil
}

func (n *namespaceNeo4jStore) getChildren(ctx context.Context, tx neo4j.Transaction, namespace domain.Namespace) ([]domain.Namespace, error) {
	res, err := tx.Run(getChildNamespacesCypher, map[string]any{
		"id": namespace.GetId(),
	})
	if err != nil {
		return nil, err
	}
	namespaces, err := n.readNamespaces(res, namespace.GetId())
	if err != nil {
		return nil, err
	}
	for _, namespace := range namespaces {
		available, err := n.quotas.GetAvailableResources(ctx, tx, namespace.GetId())
		if err != nil {
			return nil, err
		}
		err = namespace.SetAvailable(available)
		if err != nil {
			return nil, err
		}
	}
	return namespaces, nil
}

func (n *namespaceNeo4jStore) populateTree(ctx context.Context, tx neo4j.Transaction, node *domain.NamespaceTreeNode) error {
	err := n.populateTreeNode(ctx, tx, node)
	if err != nil {
		return err
	}
	for _, child := range node.Children {
		err = n.populateTree(ctx, tx, child)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *namespaceNeo4jStore) populateTreeNode(ctx context.Context, tx neo4j.Transaction, node *domain.NamespaceTreeNode) error {
	apps, err := n.apps.FindChildren(ctx, *node.Namespace)
	if err != nil {
		return err
	}
	node.Apps = apps
	children, err := n.getChildren(ctx, tx, *node.Namespace)
	if err != nil {
		return err
	}
	for _, child := range children {
		node.Children = append(node.Children, &domain.NamespaceTreeNode{Namespace: &child})
	}
	return nil
}

func (n *namespaceNeo4jStore) readNamespaces(res neo4j.Result, id string) ([]domain.Namespace, error) {
	namespaces := make([]domain.Namespace, 0)
	if res.Err() != nil {
		return namespaces, res.Err()
	}
	records, err := res.Collect()
	if err != nil {
		return namespaces, err
	}
	for _, record := range records {
		propertiesAny, found := record.Get("properties")
		if !found {
			return namespaces, fmt.Errorf("namespace %s has no properties", id)
		}
		if propertiesAny == nil {
			continue
		}
		properties, ok := propertiesAny.(map[string]any)
		if !ok {
			return namespaces, fmt.Errorf("namespace %s has no properties", id)
		}
		orgIdAny, found := properties["org_id"]
		if !found {
			return namespaces, fmt.Errorf("namespace %s has no org_id", id)
		}
		orgId, ok := orgIdAny.(string)
		if !ok {
			return namespaces, fmt.Errorf("namespace %s org_id invalid type", id)
		}
		nameAny, found := properties["name"]
		if !found {
			return namespaces, fmt.Errorf("namespace %s has no name", id)
		}
		name, ok := nameAny.(string)
		if !ok {
			return namespaces, fmt.Errorf("namespace %s name invalid type", id)
		}
		profileVersionAny, found := properties["profile_version"]
		if !found {
			return namespaces, fmt.Errorf("namespace %s has no profile_version", id)
		}
		profileVersion, ok := profileVersionAny.(string)
		if !ok {
			return namespaces, fmt.Errorf(" %s profile_version invalid type", id)
		}
		labelsAny, found := properties["labels"]
		if !found {
			return namespaces, fmt.Errorf("namespace %s has no labels", id)
		}
		labelsJson, ok := labelsAny.(string)
		if !ok {
			return namespaces, fmt.Errorf("namespace %s labels invalid type", id)
		}
		labels := make(map[string]string)
		err = json.Unmarshal([]byte(labelsJson), &labels)
		if err != nil {
			return namespaces, nil
		}
		namespace := domain.NewNamespace(orgId, name, profileVersion, labels)
		for _, resourceName := range domain.SupportedResourceQuotas {
			quotaAny, found := properties[resourceName]
			if found {
				if quota, ok := quotaAny.(float64); !ok {
					log.Printf("invalid quota type for resource name %s: %v\n", resourceName, quotaAny)
				} else {
					if err := namespace.AddResourceQuota(resourceName, quota); err != nil {
						log.Println(err)
					}
				}
			}
		}
		namespaces = append(namespaces, namespace)
	}
	return namespaces, nil
}

const addNamespaceCypher = `
CREATE (n:Namespace:Entity{id: $id, org_id: $org_id, name: $name, profile_version: $profile_version, labels: $labels});
`

const connectNamespacesCypher = `
MATCH (p:Namespace{id: $parent_id})
MATCH (c:Namespace{id: $child_id})
CREATE (p)-[:CHILD]->(c);
`

const removeNamespaceCypher = `
MATCH (n:Namespace{id: $id})
DETACH DELETE n;
`

const getNamespaceCypher = `
MATCH (n:Namespace{id: $id})
RETURN properties(n) AS properties;
`

const getChildNamespacesCypher = `
MATCH (n:Namespace{id: $id})
OPTIONAL MATCH (c:Namespace)<-[:CHILD]-(n)
RETURN properties(c) AS properties;
`
