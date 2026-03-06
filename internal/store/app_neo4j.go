package store

import (
	"context"
	"fmt"
	"log"

	"github.com/c12s/meridian/internal/domain"
	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
	"go.opentelemetry.io/otel"
)

type appNeo4jStore struct {
	driver neo4j.Driver
	dbName string
	quotas domain.ResourceQuotaStore
}

func NewAppNeo4jStore(driver neo4j.Driver, dbName string, quotas domain.ResourceQuotaStore) domain.AppStore {
	if driver == nil {
		log.Fatalln("driver while initializing app neo4j store")
	}
	return &appNeo4jStore{
		driver: driver,
		dbName: dbName,
		quotas: quotas,
	}
}

func (a *appNeo4jStore) Add(ctx context.Context, app domain.App) error {
	tracer := otel.Tracer("meridian.AppNeo4jStore")
	ctx, span := tracer.Start(ctx, "Add")
	defer span.End()

	session := startSession(a.driver, a.dbName)
	defer endSession(session)
	tx, err := session.BeginTransaction()
	if err != nil {
		return err
	}

	_, err = tx.Run(addAppCypher, map[string]any{
		"id":              app.GetId(),
		"name":            app.GetName(),
		"profile_version": app.GetProfileVersion(),
		"namespace_id":    app.GetNamespace().GetId(),
	})
	if err != nil {
		tx.Rollback()
		return err
	}

	err = a.quotas.SetResourceQuotas(ctx, app.GetId(), app.GetResourceQuotas(), tx)
	if err != nil {
		tx.Rollback()
		return err
	}
	tx.Commit()
	return nil
}

func (a *appNeo4jStore) FindChildren(ctx context.Context, namespace domain.Namespace) ([]domain.App, error) {
	tracer := otel.Tracer("meridian.AppNeo4jStore")
	ctx, span := tracer.Start(ctx, "FindChildren")
	defer span.End()

	session := startSession(a.driver, a.dbName)
	defer endSession(session)
	tx, err := session.BeginTransaction()
	if err != nil {
		return nil, err
	}
	defer tx.Commit()
	res, err := tx.Run(getChildAppsCypher, map[string]any{
		"id": namespace.GetId(),
	})
	if err != nil {
		return nil, err
	}

	return a.readApps(res, namespace)
}

func (a *appNeo4jStore) Remove(ctx context.Context, id string) error {
	tracer := otel.Tracer("meridian.AppNeo4jStore")
	ctx, span := tracer.Start(ctx, "Remove")
	defer span.End()

	session := startSession(a.driver, a.dbName)
	defer endSession(session)
	tx, err := session.BeginTransaction()
	if err != nil {
		return err
	}
	_, err = tx.Run(removeAppCypher, map[string]any{
		"id": id,
	})
	if err != nil {
		tx.Rollback()
		return err
	}
	tx.Commit()
	return nil
}

func (a *appNeo4jStore) readApps(res neo4j.Result, namespace domain.Namespace) ([]domain.App, error) {
	apps := make([]domain.App, 0)
	if res.Err() != nil {
		return apps, res.Err()
	}
	records, err := res.Collect()
	if err != nil {
		return apps, err
	}
	for _, record := range records {
		propertiesAny, found := record.Get("properties")
		if !found {
			return apps, fmt.Errorf("app has no properties")
		}
		if propertiesAny == nil {
			continue
		}
		properties, ok := propertiesAny.(map[string]any)
		if !ok {
			return apps, fmt.Errorf("app has no properties")
		}
		nameAny, found := properties["name"]
		if !found {
			return apps, fmt.Errorf("app has no name")
		}
		name, ok := nameAny.(string)
		if !ok {
			return apps, fmt.Errorf("app name invalid type")
		}
		profileVersionAny, found := properties["profile_version"]
		if !found {
			return apps, fmt.Errorf("app has no profile_version")
		}
		profileVersion, ok := profileVersionAny.(string)
		if !ok {
			return apps, fmt.Errorf("app profile_version invalid type")
		}
		app := domain.NewApp(namespace, name, profileVersion)
		for _, resourceName := range domain.SupportedResourceQuotas {
			quotaAny, found := properties[resourceName]
			if found {
				if quota, ok := quotaAny.(float64); !ok {
					log.Printf("invalid quota type for resource name %s: %v\n", resourceName, quotaAny)
				} else {
					if err := app.AddResourceQuota(resourceName, quota); err != nil {
						log.Println(err)
					}
				}
			}
		}
		apps = append(apps, app)
	}
	return apps, nil
}

const addAppCypher = `
MATCH (n:Namespace{id: $namespace_id})
CREATE (a:App:Entity{id: $id, name: $name, profile_version: $profile_version})
CREATE (n)-[:CHILD]->(a);
`

const removeAppCypher = `
MATCH (a:App{id: $id})
DETACH DELETE a;
`

const getChildAppsCypher = `
MATCH (n:Namespace{id: $id})
OPTIONAL MATCH (c:App)<-[:CHILD]-(n)
RETURN properties(c) AS properties;
`
