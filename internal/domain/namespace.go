package domain

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"slices"
)

const (
	SECCOMP_APP_WILDCARD = "*"
	SECCOMP_DEFAULT_ARCH = "x86"
)

type SeccompProfile struct {
	Namespace    string
	Application  string
	Name         string
	Version      string
	Architecture string
	NsOrgId      string
}

func MakeNamespaceId(orgId, namespaceName string) string {
	return fmt.Sprintf("%s/%s", orgId, namespaceName)
}

type Namespace struct {
	orgId          string
	name           string
	resourceQuotas ResourceQuotas
	available      ResourceQuotas
	profileVersion string
	labels         map[string]string
}

func NewNamespace(orgId, name, profileVersion string, labels map[string]string) Namespace {
	return Namespace{
		orgId:          orgId,
		name:           name,
		profileVersion: profileVersion,
		labels:         labels,
		resourceQuotas: make(ResourceQuotas, 0),
		available:      make(ResourceQuotas, 0),
	}
}

func (n Namespace) GetOrgId() string {
	return n.orgId
}

func (n Namespace) GetName() string {
	return n.name
}

func (n Namespace) GetResourceQuotas() ResourceQuotas {
	quotas := make(ResourceQuotas)
	maps.Copy(quotas, n.resourceQuotas)
	return quotas
}

func (n *Namespace) AddResourceQuota(resource string, quota float64) error {
	if !slices.Contains(SupportedResourceQuotas, resource) {
		return fmt.Errorf("quotas for a resource with name %s are not supported", resource)
	}
	n.resourceQuotas[resource] = quota
	return nil
}

func (n Namespace) GetAvailable() ResourceQuotas {
	quotas := make(ResourceQuotas)
	maps.Copy(quotas, n.available)
	return quotas
}

func (n *Namespace) SetAvailable(available ResourceQuotas) error {
	for resource := range available {
		if !slices.Contains(SupportedResourceQuotas, resource) {
			return fmt.Errorf("quotas for a resource with name %s are not supported", resource)
		}
	}
	maps.Copy(n.available, available)
	return nil
}

func (n Namespace) GetUtilized() ResourceQuotas {
	quotas := make(ResourceQuotas)
	maps.Copy(quotas, n.resourceQuotas)
	for resource, available := range n.available {
		quotas[resource] = quotas[resource] - available
	}
	return quotas
}

func (n Namespace) GetProfileVersion() string {
	return n.profileVersion
}

func (n Namespace) GetId() string {
	return MakeNamespaceId(n.orgId, n.name)
}

func (n Namespace) GetLabels() map[string]string {
	return n.labels
}

func (n Namespace) GetLabelsJson() string {
	labels, err := json.Marshal(n.labels)
	if err != nil {
		log.Println(err)
	}
	return string(labels)
}

func (n Namespace) GetSeccompProfile() SeccompProfile {
	return SeccompProfile{
		Namespace:    n.GetId(),
		Application:  SECCOMP_APP_WILDCARD,
		Name:         fmt.Sprintf("%s profile", n.GetId()),
		Version:      n.profileVersion,
		Architecture: SECCOMP_DEFAULT_ARCH,
		NsOrgId:      n.GetOrgId(),
	}
}

func (n *Namespace) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		OrgId          string            `json:"org_id"`
		Name           string            `json:"name"`
		SeccompProfile SeccompProfile    `json:"seccomp_profile"`
		ResourceQuotas ResourceQuotas    `json:"resource_quotas"`
		Labels         map[string]string `json:"labels"`
	}{
		OrgId:          n.orgId,
		Name:           n.name,
		SeccompProfile: n.GetSeccompProfile(),
		ResourceQuotas: n.resourceQuotas,
		Labels:         n.labels,
	})
}

type NamespaceTreeNode struct {
	Namespace *Namespace           `json:"namespace"`
	Apps      []App                `json:"applications"`
	Children  []*NamespaceTreeNode `json:"child_namespaces"`
}

type NamespaceTree struct {
	Root NamespaceTreeNode
}

type NamespaceStore interface {
	Add(namespace Namespace, parent *Namespace) error
	Get(id string) (Namespace, error)
	GetHierarchy(rootId string) (NamespaceTree, error)
	Remove(id string) error
}
