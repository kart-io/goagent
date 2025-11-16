// Package main demonstrates real-world usage of store adapters with common options
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/kart-io/goagent/store/adapters"
	"github.com/kart-io/k8s-agent/common/options"
	"github.com/kart-io/logger"
)

// AppConfig represents the application configuration
type AppConfig struct {
	// Store configuration
	Store *adapters.StoreOptions `mapstructure:"store"`

	// Shared Redis configuration for both store and cache
	Redis *options.RedisOptions `mapstructure:"redis"`

	// Shared MySQL configuration for both store and main database
	MySQL *options.MySQLOptions `mapstructure:"mysql"`

	// Shared PostgreSQL configuration for both store and database
	Postgres *options.PostgresOptions `mapstructure:"postgres"`
}

func main() {
	// Create configuration
	config := &AppConfig{
		Store:    adapters.NewStoreOptions(),
		Redis:    options.NewRedisOptions(),
		MySQL:    options.NewMySQLOptions(),
		Postgres: options.NewPostgresOptions(),
	}

	// Setup command-line flags
	fs := pflag.NewFlagSet("example", pflag.ContinueOnError)

	// Add store type flag
	fs.StringVar(&config.Store.Type, "store.type", "memory", "Store type (memory, redis, postgres)")
	fs.StringVar(&config.Store.Prefix, "store.prefix", "app:", "Store key prefix")
	fs.StringVar(&config.Store.TableName, "store.table", "app_stores", "Store table name for SQL stores")

	// Add Redis flags
	config.Redis.AddFlags(fs)

	// Add MySQL flags
	config.MySQL.AddFlags(fs)

	// Add PostgreSQL flags
	config.Postgres.AddFlags(fs)

	// Parse command line
	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Fatal(err)
	}

	// Load from config file if exists
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")

	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())

		// Unmarshal configuration
		if err := viper.Unmarshal(config); err != nil {
			log.Fatal("Failed to unmarshal config:", err)
		}
	}

	// Environment overrides
	viper.AutomaticEnv()
	config.Store.Type = viper.GetString("STORE_TYPE")
	if config.Store.Type == "" {
		config.Store.Type = "memory"
	}

	// Configure store based on type
	switch config.Store.Type {
	case "redis":
		// Use shared Redis configuration
		config.Store.Redis = config.Redis

		// Complete Redis configuration
		if err := config.Redis.Complete(); err != nil {
			log.Fatal("Failed to complete Redis config:", err)
		}

		// Health check
		ctx := context.Background()
		logger, err := logger.NewWithDefaults()
		if err != nil {
			log.Printf("WARNING: Failed to create logger: %v", err)
		} else {
			if err := config.Redis.Health(ctx, logger); err != nil {
				log.Printf("WARNING: Redis health check failed: %v", err)
			}
		}

	case "postgres":
		// Use shared PostgreSQL configuration
		config.Store.Postgres = config.Postgres

		// Complete PostgreSQL configuration
		if err := config.Postgres.Complete(); err != nil {
			log.Fatal("Failed to complete Postgres config:", err)
		}

		// Override with environment variables if needed
		if host := viper.GetString("POSTGRES_HOST"); host != "" {
			config.Store.Postgres.Host = host
		}
		if port := viper.GetInt("POSTGRES_PORT"); port > 0 {
			config.Store.Postgres.Port = port
		}
	}

	// Create store
	store, err := adapters.NewStore(config.Store)
	if err != nil {
		log.Fatal("Failed to create store:", err)
	}

	fmt.Printf("Successfully created %s store\n", config.Store.Type)

	// Use the store
	ctx := context.Background()

	// Store some data
	err = store.Put(ctx, []string{"app", "sessions"}, "session123", map[string]interface{}{
		"user_id":    "user456",
		"created_at": "2024-01-01T00:00:00Z",
		"data": map[string]interface{}{
			"theme":    "dark",
			"language": "en",
		},
	})
	if err != nil {
		log.Printf("Failed to store data: %v", err)
	} else {
		fmt.Println("Data stored successfully")
	}

	// Retrieve data
	value, err := store.Get(ctx, []string{"app", "sessions"}, "session123")
	if err != nil {
		log.Printf("Failed to retrieve data: %v", err)
	} else {
		fmt.Printf("Retrieved value: %+v\n", value.Value)
	}

	// List keys
	keys, err := store.List(ctx, []string{"app", "sessions"})
	if err != nil {
		log.Printf("Failed to list keys: %v", err)
	} else {
		fmt.Printf("Keys in namespace: %v\n", keys)
	}

	fmt.Println("Example completed successfully!")
}

/* Example config.yaml:

store:
  type: redis
  prefix: "myapp:store:"
  table_name: "app_stores"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0
  pool_size: 10
  min_idle_conns: 5

mysql:
  host: "localhost"
  port: 3306
  user: "root"
  password: "secret"
  database: "myapp"
  auto_migrate: true

postgres:
  host: "localhost"
  port: 5432
  user: "postgres"
  password: "secret"
  database: "myapp"
  ssl_mode: "prefer"
  auto_migrate: true

*/

/* Example command line usage:

# Use in-memory store (default)
./example

# Use Redis store
./example --store.type=redis --redis.addr=localhost:6379

# Use Redis with custom prefix
./example --store.type=redis --redis.addr=localhost:6379 --store.prefix=myapp:

# Use PostgreSQL store
./example --store.type=postgres --postgres.host=localhost --postgres.port=5432 \
          --postgres.user=postgres --postgres.password=secret --postgres.database=myapp

# Use PostgreSQL with SSL
./example --store.type=postgres --postgres.host=localhost --postgres.ssl-mode=require \
          --postgres.user=postgres --postgres.password=secret --postgres.database=myapp

# Use MySQL store (when implemented)
./example --store.type=mysql --mysql.host=localhost --mysql.port=3306 \
          --mysql.user=root --mysql.password=secret --mysql.database=myapp

# With environment variables
STORE_TYPE=redis REDIS_ADDR=redis:6379 ./example

STORE_TYPE=postgres POSTGRES_HOST=db.example.com POSTGRES_PORT=5432 ./example

*/
