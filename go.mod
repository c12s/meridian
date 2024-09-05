module github.com/c12s/meridian

go 1.22.3

require (
	github.com/c12s/gravity v1.0.0
	github.com/c12s/magnetar v1.0.0
	github.com/c12s/oort v1.0.0
	github.com/c12s/pulsar v1.0.0
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/nats-io/nats.go v1.31.0
	github.com/neo4j/neo4j-go-driver/v4 v4.4.7
	google.golang.org/grpc v1.65.0
	google.golang.org/protobuf v1.34.1
)

require (
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/klauspost/compress v1.17.0 // indirect
	github.com/nats-io/nkeys v0.4.5 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.23.0 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240528184218-531527333157 // indirect
)

replace github.com/c12s/pulsar => ../pulsar

replace github.com/c12s/oort => ../oort

replace github.com/c12s/magnetar => ../magnetar

replace github.com/c12s/gravity => ../gravity
