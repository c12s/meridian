package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	gravityapi "github.com/c12s/gravity/pkg/api"
	magnetarapi "github.com/c12s/magnetar/pkg/api"
	"github.com/c12s/meridian/internal/handlers"
	"github.com/c12s/meridian/internal/services"
	"github.com/c12s/meridian/internal/store"
	"github.com/c12s/meridian/pkg/api"
	oortapi "github.com/c12s/oort/pkg/api"
	pulsar_api "github.com/c12s/pulsar/model/protobuf"
	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
)

func main() {
	neo4jAddress := os.Getenv("NEO4J_ADDRESS")
	driver, err := neo4j.NewDriver(fmt.Sprintf("bolt://%s", neo4jAddress), neo4j.NoAuth())
	if err != nil {
		log.Fatal(err)
	}
	dbName := os.Getenv("NEO4J_DB_NAME")

	quotas := store.NewResourceQuotaNeo4jStore(driver, dbName)
	apps := store.NewAppNeo4jStore(driver, dbName, quotas)
	namespaces := store.NewNamespaceNeo4jStore(driver, dbName, quotas, apps)
	conn, err := grpc.NewClient(os.Getenv("PULSAR_ADDRESS"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	pulsar := pulsar_api.NewSeccompServiceClient(conn)
	connGravity, err := grpc.NewClient(os.Getenv("GRAVITY_ADDRESS"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	gravity := gravityapi.NewAgentQueueClient(connGravity)
	administrator, err := oortapi.NewAdministrationAsyncClient(os.Getenv("NATS_ADDRESS"))
	if err != nil {
		log.Fatalln(err)
	}
	connMagnetar, err := grpc.NewClient(os.Getenv("MAGNETAR_ADDRESS"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	magnetar := magnetarapi.NewMagnetarClient(connMagnetar)
	authorizer := services.NewAuthZService(os.Getenv("SECRET_KEY"))
	meridian := handlers.NewMeridianGrpcHandler(namespaces, apps, pulsar, quotas, administrator, gravity, magnetar, authorizer)

	//s := grpc.NewServer()
	s := grpc.NewServer(grpc.UnaryInterceptor(handlers.GetAuthInterceptor()))
	api.RegisterMeridianServer(s, meridian)
	reflection.Register(s)

	lis, err := net.Listen("tcp", os.Getenv("LISTEN_ADDRESS"))
	if err != nil {
		log.Fatal(err)
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		log.Printf("server listening at %v", lis.Addr())
		if err := s.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	<-shutdown

	s.GracefulStop()
}
