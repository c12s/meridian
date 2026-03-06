package api

import (
	context "context"
	"fmt"
	"log"

	"github.com/c12s/magnetar/pkg/messaging"
	"github.com/c12s/magnetar/pkg/messaging/nats"
	natsgo "github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"google.golang.org/protobuf/proto"
)

type MeridianAsyncClient struct {
	subscriber messaging.Subscriber
	publisher  messaging.Publisher
}

func NewMeridianAsyncClient(address, nodeId string) (*MeridianAsyncClient, error) {
	conn, err := natsgo.Connect(fmt.Sprintf("nats://%s", address))
	if err != nil {
		return nil, err
	}
	subscriber, err := nats.NewSubscriber(conn, Subject(nodeId), nodeId)
	if err != nil {
		return nil, err
	}
	publisher, err := nats.NewPublisher(conn)
	if err != nil {
		return nil, err
	}
	return &MeridianAsyncClient{
		subscriber: subscriber,
		publisher:  publisher,
	}, nil
}

func (c *MeridianAsyncClient) ReceiveConfig(handler ApplyAppConfigHandler) error {
	return c.subscriber.Subscribe(func(ctx context.Context, msg []byte, replySubject string) {
		tracer := otel.Tracer("meridian.async")
		ctx, span := tracer.Start(ctx, "ReceiveConfig")
		defer span.End()

		cmd := &ApplyAppConfigCommand{}
		err := proto.Unmarshal(msg, cmd)
		if err != nil {
			span.RecordError(err)
			log.Println(err)
			return
		}
		err = handler(ctx, cmd.OrgId, cmd.NamespaceName, cmd.AppName, cmd.SeccompProfile, cmd.Strategy, cmd.Quotas)
		if err != nil {
			span.RecordError(err)
			log.Println(err)
		}
	})
}

func (c *MeridianAsyncClient) GracefulStop() {
	err := c.subscriber.Unsubscribe()
	if err != nil {
		log.Println(err)
	}
}

type ApplyAppConfigHandler func(ctx context.Context, orgId, namespaceName, appName, seccompProfile, strategy string, quotas map[string]float64) error

func Subject(nodeId string) string {
	return fmt.Sprintf("%s.app_config", nodeId)
}
