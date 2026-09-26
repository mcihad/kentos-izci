module github.com/mcihad/kentos-izci

go 1.26.1

require (
	github.com/caarlos0/env/v11 v11.4.1
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.11.0
	github.com/joho/godotenv v1.5.1
	github.com/pressly/goose/v3 v3.28.0
	github.com/spf13/cobra v1.10.2
	github.com/vgarvardt/pgx-google-uuid/v5 v5.6.0
)

require (
	connectrpc.com/cors v0.1.0 // indirect
	github.com/andybalholm/brotli v1.2.5 // indirect
	github.com/boyter/go-string v1.0.5 // indirect
	github.com/getsentry/sentry-go v0.49.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/klauspost/compress v1.20.1 // indirect
	github.com/medama-io/go-useragent v1.2.5 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260921155816-b14227669459 // indirect
)

require (
	buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go v1.36.12-20260825204119-511051f7f437.1
	connectrpc.com/connect v1.21.0
	github.com/BurntSushi/toml v1.4.1-0.20240526193622-a339e1f7089c // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mcihad/kentos-izci/sdk/go v0.0.0
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.4.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/exp/typeparams v0.0.0-20231108232855-2478ac86f678 // indirect
	golang.org/x/mod v0.39.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/tools v0.49.0 // indirect
	google.golang.org/protobuf v1.36.12
	honnef.co/go/tools v0.8.1 // indirect
)

tool (
	connectrpc.com/connect/cmd/protoc-gen-connect-go
	google.golang.org/protobuf/cmd/protoc-gen-go
	honnef.co/go/tools/cmd/staticcheck
)

replace github.com/mcihad/kentos-izci/sdk/go => ./sdk/go
