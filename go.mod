module github.com/c12s/meridian

go 1.22.3

require (
	github.com/neo4j/neo4j-go-driver/v4 v4.4.7
	google.golang.org/grpc v1.65.0
	google.golang.org/protobuf v1.34.1
	github.com/c12s/pulsar v1.0.0
)

require (
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240528184218-531527333157 // indirect
)

replace github.com/c12s/pulsar => ../pulsar