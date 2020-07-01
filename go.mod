module github.com/c12s/meridian

go 1.14

replace github.com/coreos/go-systemd => github.com/coreos/go-systemd/v22 v22.0.0

replace github.com/coreos/etcd => github.com/ozonru/etcd v3.3.20-grpc1.27-origmodule+incompatible

require (
	github.com/c12s/apollo v0.0.0-20191225203730-f26e394a8564
	github.com/c12s/scheme v0.0.0-20200522155846-e2494a83b98c
	github.com/c12s/stellar-go v0.0.0-20191220161710-a82c2c7bb52e
	github.com/coreos/etcd v3.3.22+incompatible
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf // indirect
	github.com/coreos/pkg v0.0.0-20180928190104-399ea9e2e55f // indirect
	github.com/go-redis/redis v6.15.8+incompatible
	github.com/gogo/protobuf v1.3.1 // indirect
	github.com/golang/protobuf v1.4.2
	github.com/google/uuid v1.1.1 // indirect
	github.com/nats-io/nkeys v0.1.4 // indirect
	go.uber.org/zap v1.15.0 // indirect
	golang.org/x/crypto v0.0.0-20200510223506-06a226fb4e37 // indirect
	golang.org/x/lint v0.0.0-20200302205851-738671d3881b // indirect
	golang.org/x/net v0.0.0-20200520182314-0ba52f642ac2
	golang.org/x/sys v0.0.0-20200519105757-fe76b779f299 // indirect
	golang.org/x/text v0.3.2 // indirect
	golang.org/x/tools v0.0.0-20200521211927-2b542361a4fc // indirect
	google.golang.org/genproto v0.0.0-20200521103424-e9a78aa275b7 // indirect
	google.golang.org/grpc v1.29.1
	gopkg.in/yaml.v2 v2.3.0
	honnef.co/go/tools v0.0.1-2020.1.4 // indirect
)
