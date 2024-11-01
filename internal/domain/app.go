package domain

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

func MakeAppId(orgId, namespaceName, appName string) string {
	return fmt.Sprintf("%s/%s", MakeNamespaceId(orgId, namespaceName), appName)
}

type App struct {
	namespace      Namespace
	name           string
	resourceQuotas ResourceQuotas
	profileVersion string
}

func NewApp(namespace Namespace, name, profileVersion string) App {
	return App{
		namespace:      namespace,
		name:           name,
		profileVersion: profileVersion,
		resourceQuotas: make(ResourceQuotas),
	}
}

func (a App) GetNamespace() Namespace {
	return a.namespace
}

func (a App) GetName() string {
	return a.name
}

func (a App) GetId() string {
	return MakeAppId(a.namespace.orgId, a.namespace.name, a.name)
}

func (a App) GetProfileVersion() string {
	return a.profileVersion
}

func (a App) GetResourceQuotas() ResourceQuotas {
	quotas := make(ResourceQuotas)
	maps.Copy(quotas, a.resourceQuotas)
	return quotas
}

func (a *App) AddResourceQuota(resource string, quota float64) error {
	if !slices.Contains(SupportedResourceQuotas, resource) {
		return fmt.Errorf("quotas for a resource with name %s are not supported", resource)
	}
	a.resourceQuotas[resource] = quota
	return nil
}

func (a App) GetSeccompProfile() SeccompProfile {
	return SeccompProfile{
		Namespace:    a.GetNamespace().GetId(),
		Application:  a.GetName(),
		Name:         fmt.Sprintf("%s profile", a.GetId()),
		Version:      a.profileVersion,
		Architecture: SECCOMP_DEFAULT_ARCH,
		NsOrgId:      a.GetNamespace().GetOrgId(),
	}
}

func (a *App) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Name           string         `json:"name"`
		SeccompProfile SeccompProfile `json:"seccomp_profile"`
		ResourceQuotas ResourceQuotas `json:"resource_quotas"`
	}{
		Name:           a.name,
		SeccompProfile: a.GetSeccompProfile(),
		ResourceQuotas: a.resourceQuotas,
	})
}

type AppStore interface {
	Add(app App) error
	FindChildren(namespace Namespace) ([]App, error)
	Remove(id string) error
}
