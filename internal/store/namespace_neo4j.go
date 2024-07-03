package store

import (
	"fmt"
	"log"

	"github.com/c12s/meridian/internal/domain"
	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
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

func (n *namespaceNeo4jStore) Add(namespace domain.Namespace, parent *domain.Namespace) error {
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
	})
	if err != nil {
		tx.Rollback()
		return err
	}

	if parent != nil {
		_, err = tx.Run(connectNamespacesCypher, map[string]any{
			"parent_id": namespace.GetId(),
			"child_id":  parent.GetId(),
		})
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	err = n.quotas.SetResourceQuotas(namespace.GetId(), namespace.GetResourceQuotas(), tx)
	if err != nil {
		tx.Rollback()
		return err
	}
	tx.Commit()
	return nil
}

func (n *namespaceNeo4jStore) Get(id string) (domain.Namespace, error) {
	session := startSession(n.driver, n.dbName)
	defer endSession(session)
	tx, err := session.BeginTransaction()
	if err != nil {
		return domain.Namespace{}, err
	}
	return n.get(tx, id)
}

func (n *namespaceNeo4jStore) GetHierarchy(rootId string) (domain.NamespaceTree, error) {
	session := startSession(n.driver, n.dbName)
	defer endSession(session)
	tx, err := session.BeginTransaction()
	if err != nil {
		return domain.NamespaceTree{}, err
	}
	root, err := n.get(tx, rootId)
	if err != nil {
		tx.Rollback()
		return domain.NamespaceTree{}, err
	}
	rootNode := &domain.NamespaceTreeNode{Namespace: root}
	err = n.populateTree(tx, rootNode)
	if err != nil {
		tx.Rollback()
		return domain.NamespaceTree{}, err
	}
	return domain.NamespaceTree{Root: *rootNode}, nil
}

func (n *namespaceNeo4jStore) Remove(id string) error {
	session := startSession(n.driver, n.dbName)
	defer endSession(session)
	tx, err := session.BeginTransaction()
	if err != nil {
		return err
	}
	_, err = tx.Run(removeNamespaceCypher, map[string]any{
		"id": id,
	})
	return err
}

func (n *namespaceNeo4jStore) get(tx neo4j.Transaction, id string) (domain.Namespace, error) {
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
		return domain.Namespace{}, fmt.Errorf("cannot find parent for namespace %s", id)
	}
	return namespaces[0], nil
}

func (n *namespaceNeo4jStore) getChildren(tx neo4j.Transaction, namespace domain.Namespace) ([]domain.Namespace, error) {
	res, err := tx.Run(getChildNamespacesCypher, map[string]any{
		"id": namespace.GetId(),
	})
	if err != nil {
		return nil, err
	}
	return n.readNamespaces(res, namespace.GetId())
}

func (n *namespaceNeo4jStore) populateTree(tx neo4j.Transaction, node *domain.NamespaceTreeNode) error {
	err := n.populateTreeNode(tx, node)
	if err != nil {
		return err
	}
	for _, child := range node.Children {
		err = n.populateTree(tx, &child)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *namespaceNeo4jStore) populateTreeNode(tx neo4j.Transaction, node *domain.NamespaceTreeNode) error {
	apps, err := n.apps.FindChildren(node.Namespace)
	if err != nil {
		return err
	}
	node.Apps = apps
	children, err := n.getChildren(tx, node.Namespace)
	if err != nil {
		return err
	}
	for _, child := range children {
		node.Children = append(node.Children, domain.NamespaceTreeNode{Namespace: child})
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
		orgIdAny, found := record.Get("org_id")
		if !found {
			return namespaces, fmt.Errorf("namespace %s has no org_id", id)
		}
		orgId, ok := orgIdAny.(string)
		if !ok {
			return namespaces, fmt.Errorf("namespace %s org_id invalid type", id)
		}
		nameAny, found := record.Get("name")
		if !found {
			return namespaces, fmt.Errorf("namespace %s has no name", id)
		}
		name, ok := nameAny.(string)
		if !ok {
			return namespaces, fmt.Errorf("namespace %s name invalid type", id)
		}
		profileVersionAny, found := record.Get("profile_version")
		if !found {
			return namespaces, fmt.Errorf("namespace %s has no profile_version", id)
		}
		profileVersion, ok := profileVersionAny.(string)
		if !ok {
			return namespaces, fmt.Errorf(" %s profile_version invalid type", id)
		}
		namespace := domain.NewNamespace(orgId, name, profileVersion)
		for _, resourceName := range domain.SupportedResourceQuotas {
			quotaAny, found := record.Get(resourceName)
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
CREATE (n:Namespace:Entity{id: $id, org_id: $org_id, name: $name, profile_version: $profile_version})
`

const connectNamespacesCypher = `
MATCH (p:Namespace{id: $parent_id})
MATCH (c:Namespace{id: $child_id})
CREATE (p)-[:CHILD]->(c)
`

const removeNamespaceCypher = `
MATCH (n:Namespace{id: $id})
DETACH DELETE n
`

const getNamespaceCypher = `
MATCH (n:Namespace{id: $id})
RETURN properties(n)
`

const getChildNamespacesCypher = `
MATCH (n:Namespace{id: $id})
OPTIONAL MATCH (c:Namespace)<-[:CHILD]-(n)
RETURN properties(c)
`
