package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"
	"time"

	gravityapi "github.com/c12s/gravity/pkg/api"
	magnetarapi "github.com/c12s/magnetar/pkg/api"
	"github.com/c12s/meridian/internal/domain"
	"github.com/c12s/meridian/internal/services"
	"github.com/c12s/meridian/pkg/api"
	oortapi "github.com/c12s/oort/pkg/api"
	pulsar_api "github.com/c12s/pulsar/model/protobuf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type MeridianGrpcHandler struct {
	api.UnimplementedMeridianServer
	namespaces    domain.NamespaceStore
	apps          domain.AppStore
	resources     domain.ResourceQuotaStore
	pulsar        pulsar_api.SeccompServiceClient
	administrator *oortapi.AdministrationAsyncClient
	gravity       gravityapi.AgentQueueClient
	magnetar      magnetarapi.MagnetarClient
	authorizer    services.AuthZService
}

func NewMeridianGrpcHandler(namespaces domain.NamespaceStore, apps domain.AppStore, pulsar pulsar_api.SeccompServiceClient, resources domain.ResourceQuotaStore, administrator *oortapi.AdministrationAsyncClient, gravity gravityapi.AgentQueueClient, magnetar magnetarapi.MagnetarClient, authorizer services.AuthZService) api.MeridianServer {
	return MeridianGrpcHandler{
		namespaces:    namespaces,
		apps:          apps,
		pulsar:        pulsar,
		resources:     resources,
		administrator: administrator,
		gravity:       gravity,
		magnetar:      magnetar,
		authorizer:    authorizer,
	}
}

func (m MeridianGrpcHandler) AddNamespace(ctx context.Context, req *api.AddNamespaceReq) (*api.AddNamespaceResp, error) {
	err := m.authorizer.Authorize(ctx, "org.namespace.add", "org", req.OrgId)
	if err != nil {
		log.Printf("AddNamespace authz failed meridian org.namespace.add|org|%s", req.OrgId)
		return nil, status.Errorf(codes.PermissionDenied, err.Error())
	}

	namespace, err := m.namespaces.Get(domain.MakeNamespaceId(req.OrgId, req.Name))
	if err == nil {
		err = status.Error(codes.AlreadyExists, "namespace already exists")
		return nil, err
	}
	var parent *domain.Namespace
	if req.ParentName != "" {
		p, err := m.namespaces.Get(domain.MakeNamespaceId(req.OrgId, req.ParentName))
		if err != nil {
			log.Println(err)
			err = status.Error(codes.NotFound, "parent namespace not found")
			return nil, err
		}
		parent = &p
	}
	namespace = domain.NewNamespace(req.OrgId, req.Name, req.Profile.Version, req.Labels)
	for resource, quota := range req.Quotas {
		err := namespace.AddResourceQuota(resource, quota)
		if err != nil {
			log.Println(err)
			err = status.Error(codes.InvalidArgument, err.Error())
			return nil, err
		}
	}
	err = m.sendSeccompProfile(ctx,
		req.SeccompDefinitionStrategy,
		namespace.GetSeccompProfile(),
		req.Profile, parent)
	if err != nil {
		return nil, err
	}
	err = m.namespaces.Add(namespace, parent)
	if err != nil {
		log.Println(err)
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}
	var parentRes *oortapi.Resource
	if parent == nil {
		parentRes = &oortapi.Resource{
			Id:   req.OrgId,
			Kind: "org",
		}
	} else {
		parentRes = &oortapi.Resource{
			Id:   parent.GetId(),
			Kind: "namespace",
		}
	}
	err2 := m.administrator.SendRequest(&oortapi.CreateInheritanceRelReq{
		From: parentRes,
		To: &oortapi.Resource{
			Id:   namespace.GetId(),
			Kind: "namespace",
		},
	}, func(resp *oortapi.AdministrationAsyncResp) {
		log.Println(resp.Error)
	})
	if err2 != nil {
		log.Println(err2)
	}
	return &api.AddNamespaceResp{}, nil
}

func (m MeridianGrpcHandler) RemoveNamespace(ctx context.Context, req *api.RemoveNamespaceReq) (*api.RemoveNamespaceResp, error) {
	nsId := domain.MakeNamespaceId(req.OrgId, req.Name)
	err := m.authorizer.Authorize(ctx, "namespace.delete", "namespace", nsId)
	if err != nil {
		return nil, status.Errorf(codes.PermissionDenied, err.Error())
	}
	tree, err := m.namespaces.GetHierarchy(nsId)
	if err == nil && (len(tree.Root.Children) > 0 || len(tree.Root.Apps) > 0) {
		err = status.Error(codes.InvalidArgument, "namespace must not have applications or child namespaces")
		return nil, err
	}
	err = m.namespaces.Remove(domain.MakeNamespaceId(req.OrgId, req.Name))
	if err != nil {
		log.Println(err)
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}
	return &api.RemoveNamespaceResp{}, nil
}

// ns.get|namespace|nsId ce proveriti da li je user iz te org i da li ns pripada toj org na osnovu id
func (m MeridianGrpcHandler) AddApp(ctx context.Context, req *api.AddAppReq) (*api.AddAppResp, error) {
	nsId := domain.MakeNamespaceId(req.OrgId, req.Namespace)
	err := m.authorizer.Authorize(ctx, "org.namespace.get", "namespace", nsId)
	if err != nil {
		return nil, status.Errorf(codes.PermissionDenied, "the namespace is not associated with the organization")
	}

	err = m.authorizer.Authorize(ctx, "namespace.app.add", "namespace", nsId)
	if err != nil {
		return nil, status.Errorf(codes.PermissionDenied, err.Error())
	}

	namespace, err := m.namespaces.Get(nsId)
	if err != nil {
		log.Println(err)
		err = status.Error(codes.NotFound, "namespace not found")
		return nil, err
	}
	app := domain.NewApp(namespace, req.Name, req.Profile.Version)
	for resource, quota := range req.Quotas {
		err := app.AddResourceQuota(resource, quota)
		if err != nil {
			log.Println(err)
			err = status.Error(codes.InvalidArgument, err.Error())
			return nil, err
		}
	}
	err = m.sendSeccompProfile(ctx,
		req.SeccompDefinitionStrategy,
		app.GetSeccompProfile(),
		req.Profile, &namespace)
	if err != nil {
		return nil, err
	}
	err = m.apps.Add(app)
	if err != nil {
		log.Println(err)
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}
	nodes, err := m.placeByGossip(ctx, req.OrgId, 50)
	if err != nil {
		return nil, err
	}
	profile, err := json.MarshalIndent(m.getSeccompProfile(ctx, app.GetSeccompProfile()), "", "\t")
	if err != nil {
		return nil, err
	}
	cmd := api.ApplyAppConfigCommand{
		OrgId:          req.OrgId,
		NamespaceName:  req.Namespace,
		AppName:        req.Name,
		SeccompProfile: string(profile),
		Quotas:         req.Quotas,
	}
	cmdMarshalled, err := proto.Marshal(&cmd)
	if err != nil {
		return nil, err
	}
	ctx = setOutgoingContext(ctx)
	for _, node := range nodes {
		_, err = m.gravity.DisseminateAppConfig(ctx, &gravityapi.DeseminateConfigRequest{
			NodeId: node.Id,
			Config: cmdMarshalled,
		})
		if err != nil {
			log.Println(err)
			err = status.Error(codes.Internal, err.Error())
			return nil, err
		}
	}

	err2 := m.administrator.SendRequest(&oortapi.CreateInheritanceRelReq{
		From: &oortapi.Resource{
			Id:   nsId,
			Kind: "namespace",
		},
		To: &oortapi.Resource{
			Id:   app.GetId(),
			Kind: "app",
		},
	}, func(resp *oortapi.AdministrationAsyncResp) {
		log.Println(resp.Error)
	})
	if err2 != nil {
		log.Println(err2)
	}
	return &api.AddAppResp{}, nil
}

func (m MeridianGrpcHandler) RemoveApp(ctx context.Context, req *api.RemoveAppReq) (*api.RemoveAppResp, error) {
	nsId := domain.MakeNamespaceId(req.OrgId, req.Namespace)
	err := m.authorizer.Authorize(ctx, "org.namespace.get", "namespace", nsId)
	if err != nil {
		return nil, status.Errorf(codes.PermissionDenied, "the namespace is not associated with the organization")
	}

	appId := domain.MakeAppId(req.OrgId, req.Namespace, req.Name)
	err = m.authorizer.Authorize(ctx, "app.delete", "app", appId)
	if err != nil {
		return nil, status.Errorf(codes.PermissionDenied, err.Error())
	}

	err = m.apps.Remove(appId)
	if err != nil {
		log.Println(err)
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}
	return &api.RemoveAppResp{}, nil
}

func (m MeridianGrpcHandler) GetNamespace(ctx context.Context, req *api.GetNamespaceReq) (*api.GetNamespaceResp, error) {
	nsId := domain.MakeNamespaceId(req.OrgId, req.Name)
	namespace, err := m.namespaces.Get(nsId)
	if err != nil {
		log.Println(err)
		err = status.Error(codes.NotFound, "namespace not found")
		return nil, err
	}

	err = m.authorizer.Authorize(ctx, "org.namespace.get", "namespace", nsId)
	if err != nil {
		log.Println(err)
		return nil, status.Errorf(codes.PermissionDenied, "the namespace is not associated with user organization")
	}

	return &api.GetNamespaceResp{
		Name:      namespace.GetName(),
		Labels:    namespace.GetLabels(),
		Total:     namespace.GetResourceQuotas(),
		Available: namespace.GetAvailable(),
		Utilized:  namespace.GetUtilized(),
		Profile:   m.getSeccompProfile(ctx, namespace.GetSeccompProfile()),
	}, nil
}

func (m MeridianGrpcHandler) GetNamespaceHierarchy(ctx context.Context, req *api.GetNamespaceHierarchyReq) (*api.GetNamespaceHierarchyResp, error) {
	nsId := domain.MakeNamespaceId(req.OrgId, "default")
	err := m.authorizer.Authorize(ctx, "org.namespace.get", "namespace", nsId)
	if err != nil {
		return nil, status.Errorf(codes.PermissionDenied, "the namespace is not associated with user organization")
	}

	tree, err := m.namespaces.GetHierarchy(nsId)
	if err != nil {
		log.Println(err)
		err = status.Error(codes.NotFound, "namespace hierarchy not found")
		return nil, err
	}
	return m.mapNamespaceTreeNode(ctx, &tree.Root), nil
}

func (m MeridianGrpcHandler) SetNamespaceResources(ctx context.Context, req *api.SetNamespaceResourcesReq) (*api.SetNamespaceResourcesResp, error) {
	nsId := domain.MakeNamespaceId(req.OrgId, req.Name)
	err := m.authorizer.Authorize(ctx, "namespace.put", "namespace", nsId)
	if err != nil {
		log.Println(err)
		return nil, status.Errorf(codes.PermissionDenied, err.Error())
	}

	err = m.resources.SetResourceQuotas(nsId, domain.ResourceQuotas(req.Quotas), nil)
	if err != nil {
		log.Println(err)
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}
	return &api.SetNamespaceResourcesResp{}, nil
}

func (m MeridianGrpcHandler) SetAppResources(ctx context.Context, req *api.SetAppResourcesReq) (*api.SetAppResourcesResp, error) {
	nsId := domain.MakeNamespaceId(req.OrgId, req.Namespace)
	// proverava da li ns pripada org u kojoj je i user
	err := m.authorizer.Authorize(ctx, "org.namespace.get", "namespace", nsId)
	if err != nil {
		log.Println("SetAppResources org.namespace.get failed ")
		return nil, status.Errorf(codes.PermissionDenied, err.Error())
	}

	appId := domain.MakeAppId(req.OrgId, req.Namespace, req.Name)
	err = m.authorizer.Authorize(ctx, "app.put", "app", appId) // prava za konkretnu app u ns
	if err != nil {
		log.Println("SetAppResources app.put failed ")
		return nil, status.Errorf(codes.PermissionDenied, err.Error())
	}

	err = m.resources.SetResourceQuotas(appId, domain.ResourceQuotas(req.Quotas), nil)
	if err != nil {
		log.Println(err)
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}
	return &api.SetAppResourcesResp{}, nil
}

func (m *MeridianGrpcHandler) mapNamespaceTreeNode(ctx context.Context, node *domain.NamespaceTreeNode) *api.GetNamespaceHierarchyResp {
	resp := &api.GetNamespaceHierarchyResp{
		Namespace: &api.GetNamespaceHierarchyResp_Namespace{
			Name:      node.Namespace.GetName(),
			Labels:    node.Namespace.GetLabels(),
			Total:     node.Namespace.GetResourceQuotas(),
			Available: node.Namespace.GetAvailable(),
			Utilized:  node.Namespace.GetUtilized(),
			Profile:   m.getSeccompProfile(ctx, node.Namespace.GetSeccompProfile()),
		},
	}
	for _, app := range node.Apps {
		resp.Apps = append(resp.Apps, &api.GetNamespaceHierarchyResp_App{
			Name:    app.GetName(),
			Total:   app.GetResourceQuotas(),
			Profile: m.getSeccompProfile(ctx, app.GetSeccompProfile()),
		})
	}
	for _, child := range node.Children {
		resp.Namespaces = append(resp.Namespaces, m.mapNamespaceTreeNode(ctx, child))
	}
	return resp
}

func (m *MeridianGrpcHandler) sendSeccompProfile(ctx context.Context, strategy string, metadata domain.SeccompProfile, profileDefinition *api.SeccompProfile, parent *domain.Namespace) error {
	ctx = setOutgoingContext(ctx)
	switch strings.ToLower(strategy) {
	case "redefine":
		profile := &pulsar_api.SeccompProfileDefinitionRequest{
			Profile: &pulsar_api.SeccompProfile{
				Namespace:    metadata.Namespace,
				Application:  metadata.Application,
				Name:         metadata.Name,
				Version:      metadata.Version,
				Architecture: metadata.Architecture,
			},
			Definition: &pulsar_api.SeccompProfileDefinition{
				DefaultAction: profileDefinition.DefaultAction,
				Architectures: []string{metadata.Architecture},
			},
		}
		for _, syscall := range profileDefinition.Syscalls {
			profile.Definition.Syscalls = append(profile.Definition.Syscalls, &pulsar_api.Syscalls{
				Names:  syscall.Names,
				Action: syscall.Action,
			})
		}
		_, err := m.pulsar.DefineSeccompProfile(ctx, profile)
		if err != nil {
			return err
		}
	case "extend":
		if parent == nil {
			return status.Error(codes.InvalidArgument, "cannot inherit or extend seccomp profiles - there is no parent")
		}
		profile := &pulsar_api.ExtendSeccompProfileRequest{
			ExtendProfile: &pulsar_api.SeccompProfile{
				Namespace:    parent.GetId(),
				Application:  parent.GetSeccompProfile().Application,
				Name:         parent.GetSeccompProfile().Name,
				Version:      parent.GetSeccompProfile().Version,
				Architecture: parent.GetSeccompProfile().Architecture,
			},
			DefineProfile: &pulsar_api.SeccompProfile{
				Namespace:    metadata.Namespace,
				Application:  metadata.Application,
				Name:         metadata.Name,
				Version:      metadata.Version,
				Architecture: metadata.Architecture,
			},
		}
		for _, syscall := range profileDefinition.Syscalls {
			profile.Syscalls = append(profile.Syscalls, &pulsar_api.Syscalls{
				Names:  syscall.Names,
				Action: syscall.Action,
			})
		}
		_, err := m.pulsar.ExtendSeccompProfile(ctx, profile)
		if err != nil {
			return err
		}
	// inherit
	default:
		if parent == nil {
			return status.Error(codes.InvalidArgument, "cannot inherit or extend seccomp profiles - there is no parent")
		}
		profile, err := m.pulsar.GetSeccompProfile(ctx, &pulsar_api.SeccompProfile{
			Namespace:    parent.GetSeccompProfile().Namespace,
			Application:  parent.GetSeccompProfile().Application,
			Name:         parent.GetSeccompProfile().Name,
			Version:      parent.GetSeccompProfile().Version,
			Architecture: parent.GetSeccompProfile().Architecture,
		})
		if err != nil {
			return err
		}
		_, err = m.pulsar.DefineSeccompProfile(ctx, &pulsar_api.SeccompProfileDefinitionRequest{
			Profile: &pulsar_api.SeccompProfile{
				Namespace:    metadata.Namespace,
				Application:  metadata.Application,
				Name:         metadata.Name,
				Version:      metadata.Version,
				Architecture: metadata.Architecture,
			},
			Definition: profile.Definition,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *MeridianGrpcHandler) getSeccompProfile(ctx context.Context, metadata domain.SeccompProfile) *api.SeccompProfile {
	ctx = setOutgoingContext(ctx)
	resp, err := m.pulsar.GetSeccompProfile(ctx, &pulsar_api.SeccompProfile{
		Namespace:    metadata.Namespace,
		Application:  metadata.Application,
		Name:         metadata.Name,
		Version:      metadata.Version,
		Architecture: metadata.Architecture,
	})
	if err != nil {
		log.Println(err)
		return nil
	}
	profile := &api.SeccompProfile{
		Version:       metadata.Version,
		DefaultAction: resp.Definition.DefaultAction,
	}
	for _, syscall := range resp.Definition.Syscalls {
		profile.Syscalls = append(profile.Syscalls, &api.SyscallRule{
			Names:  syscall.Names,
			Action: syscall.Action,
		})
	}
	return profile
}

func (m *MeridianGrpcHandler) placeByGossip(ctx context.Context, org string, percentage int32) ([]*magnetarapi.NodeStringified, error) {
	queryReq := &magnetarapi.ListOrgOwnedNodesReq{
		Org: string(org),
	}
	ctx = setOutgoingContext(ctx)
	queryResp, err := m.magnetar.ListOrgOwnedNodes(ctx, queryReq)
	if err != nil {
		return nil, err
	}

	fmt.Printf("queryResp.Nodes: %+v\n", queryResp.Nodes)

	nodes := selectRandmNodes(queryResp.Nodes, percentage)
	return nodes, nil
}

func selectRandmNodes(nodes []*magnetarapi.NodeStringified, percentage int32) []*magnetarapi.NodeStringified {
	totalNodes := len(nodes)
	numberOfNodesToSelect := int(math.Ceil(float64(totalNodes) * float64(percentage) / 100))

	r := rand.New(rand.NewSource(time.Now().Unix()))

	selectedNodes := make([]*magnetarapi.NodeStringified, 0)

	for i := 0; i < numberOfNodesToSelect; i++ {
		index := r.Intn(len(nodes))
		selectedNodes = append(selectedNodes, nodes[index])
		nodes = append(nodes[:index], nodes[index+1:]...)
	}

	return selectedNodes
}

func (m MeridianGrpcHandler) BorrowResources(ctx context.Context, req *api.BorrowResourcesReq) (*api.BorrowResourcesResp, error) {
	// borrow resources from app1 to app2
	// app1 must release all allocated resources.
	app1Quotas, err := m.resources.GetQuotas(nil, req.App1Id)
	if err != nil {
		return nil, err
	}
	app1Quotas[domain.SupportedResourceQuotas[2]] = 0
	err = m.resources.SetResourceQuotas(req.App1Id, app1Quotas, nil)
	if err != nil {
		return nil, err
	}

	resourceQuotas, err := m.resources.GetAvailableResources(nil, req.App2Id)
	if err != nil {
		return nil, err
	}
	diskApp2, ok := resourceQuotas[domain.SupportedResourceQuotas[2]]
	if !ok {
		err = status.Error(codes.InvalidArgument, "no disk resources available for app2")
		return nil, err
	}

	if diskApp2 >= req.DiskResources {
		//do nothing because app2 already has enough resources
		return &api.BorrowResourcesResp{Reply: "Application 2 already has enough disk resources", Done: true}, nil
	}

	ns2ResourceQuotas, err := m.resources.GetAvailableResources(nil, req.Namespace2Id)

	if err != nil {
		return nil, err
	}

	ns2Disk, ok := ns2ResourceQuotas[domain.SupportedResourceQuotas[2]]
	if !ok {
		err = status.Error(codes.InvalidArgument, "no disk resources available for namespace 2")
		return nil, err
	}

	if req.Namespace1Id == req.Namespace2Id {
		//apps are in the same namespace
		if ns2Disk >= req.DiskResources {
			newQuotas, err := m.resources.GetQuotas(nil, req.App2Id)
			if err != nil {
				return nil, err
			}
			newQuotas[domain.SupportedResourceQuotas[2]] += req.DiskResources
			err = m.resources.SetResourceQuotas(req.App2Id, newQuotas, nil)
			if err != nil {
				return nil, err
			}
			return &api.BorrowResourcesResp{Reply: "Merge - apps are in the same namespace", Done: true}, nil
		} else {
			//todo - return error?
			fmt.Println("error app1 did not return resources")
			return &api.BorrowResourcesResp{Reply: "there are not enough resources in a namespace", Done: false}, nil
		}
	}
	ns1Parent, err := m.namespaces.GetParentNamespace(req.Namespace1Id)
	if err != nil {
		return nil, err
	}

	ns2Parent, err := m.namespaces.GetParentNamespace(req.Namespace2Id)
	if err != nil {
		return nil, err
	}

	if ns1Parent.GetId() == ns2Parent.GetId() {
		// namespaces have the same parent
		if ns2Disk >= req.DiskResources {
			newQuotas, err := m.resources.GetQuotas(nil, req.App2Id)
			if err != nil {
				return nil, err
			}
			newQuotas[domain.SupportedResourceQuotas[2]] = newQuotas[domain.SupportedResourceQuotas[2]] + req.DiskResources + diskApp2
			err = m.resources.SetResourceQuotas(req.App2Id, newQuotas, nil)
			if err != nil {
				return nil, err
			}
			return &api.BorrowResourcesResp{Reply: "Namespaces have the same parent", Done: true}, nil
		}

		ns1ResourceQuotas, err := m.resources.GetAvailableResources(nil, req.Namespace1Id)
		if err != nil {
			return nil, err
		}

		ns1Disk, ok := ns1ResourceQuotas[domain.SupportedResourceQuotas[2]]
		if !ok {
			err = status.Error(codes.InvalidArgument, "no disk resources available for namespace 1")
			return nil, err
		}

		if ns1Disk < req.DiskResources {
			// todo: error
			return &api.BorrowResourcesResp{Reply: "app1 did not return resources", Done: false}, nil
		}

		ns1Quotas, err := m.resources.GetQuotas(nil, req.Namespace1Id)
		if err != nil {
			return nil, err
		}
		ns1Quotas[domain.SupportedResourceQuotas[2]] -= req.DiskResources
		err = m.resources.SetResourceQuotas(req.Namespace1Id, ns1Quotas, nil)
		if err != nil {
			return nil, err
		}

		ns2Quotas, err := m.resources.GetQuotas(nil, req.Namespace2Id)
		if err != nil {
			return nil, err
		}
		ns2Quotas[domain.SupportedResourceQuotas[2]] += req.DiskResources
		err = m.resources.SetResourceQuotas(req.Namespace2Id, ns2Quotas, nil)
		if err != nil {
			return nil, err
		}

		app2Quotas, err := m.resources.GetQuotas(nil, req.App2Id)
		if err != nil {
			return nil, err
		}
		app2Quotas[domain.SupportedResourceQuotas[2]] += req.DiskResources
		err = m.resources.SetResourceQuotas(req.App2Id, app2Quotas, nil)
		if err != nil {
			return nil, err
		}
		return &api.BorrowResourcesResp{Reply: "Namespaces have the same parent", Done: true}, nil
	}
	//todo - return error?
	return &api.BorrowResourcesResp{Reply: "Unable to borrow resources", Done: false}, nil
}

func setOutgoingContext(ctx context.Context) context.Context {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		log.Println("[WARN] no metadata in ctx when sending req")
		return ctx
	}
	return metadata.NewOutgoingContext(ctx, md)
}

func GetAuthInterceptor() func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if ok && len(md.Get("authz-token")) > 0 {
			ctx = context.WithValue(ctx, "authz-token", md.Get("authz-token")[0])
		}
		// Calls the handler
		return handler(ctx, req)
	}
}
