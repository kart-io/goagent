module github.com/kart-io/goagent/examples/fact-memory/postgres

go 1.27.0

toolchain go1.27.3

require (
	github.com/jackc/pgx/v5 v5.10.0
	github.com/kart-io/goagent v0.0.0
	github.com/pgvector/pgvector-go v0.3.0
)

require (
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

replace github.com/kart-io/goagent => ../../..
