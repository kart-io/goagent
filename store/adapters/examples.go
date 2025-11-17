// Package adapters provides example usage of store adapters
package adapters

import (
	"context"
	"fmt"

	"github.com/spf13/pflag"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/store"
	"github.com/kart-io/k8s-agent/common/options"
)

// Example 1: Using RedisStoreAdapter with common RedisOptions
func ExampleRedisStoreAdapter() {
	// Create standard Redis options from common package
	redisOpts := options.NewRedisOptions()

	// Can be populated from flags
	fs := pflag.NewFlagSet("example", pflag.ContinueOnError)
	redisOpts.AddFlags(fs)

	// Or from config file via viper
	// viper.UnmarshalKey("redis", redisOpts)

	// Create store adapter
	adapter := NewRedisStoreAdapter(redisOpts, "myapp:store:")

	// Create store instance
	store, err := adapter.CreateStore()
	if err != nil {
		panic(err)
	}

	// Use the store
	ctx := context.Background()
	_ = store.Put(ctx, []string{"users"}, "user123", map[string]interface{}{
		"name": "Alice",
		"age":  30,
	})

	fmt.Println("Store created and data saved")
}

// Example 2: Using StoreOptions for unified configuration
func ExampleStoreOptions() {
	// Create store options
	storeOpts := NewStoreOptions()
	storeOpts.Type = "redis"

	// Configure Redis through common options
	storeOpts.Redis.Addr = "localhost:6379"
	storeOpts.Redis.PoolSize = 20

	// Validate configuration
	if err := storeOpts.Redis.Validate(); err != nil {
		panic(err)
	}

	// Create store
	store, err := NewStore(storeOpts)
	if err != nil {
		panic(err)
	}

	// Use the store
	ctx := context.Background()
	value, err := store.Get(ctx, []string{"cache"}, "key1")
	if err != nil {
		fmt.Printf("Key not found: %v\n", err)
	} else {
		fmt.Printf("Value: %v\n", value)
	}
}

// Example 3: Integration with application configuration
type AppConfig struct {
	Store *StoreOptions         `mapstructure:"store"`
	Redis *options.RedisOptions `mapstructure:"redis"`
	MySQL *options.MySQLOptions `mapstructure:"mysql"`
}

func ExampleAppIntegration() {
	// Load configuration (e.g., from YAML file)
	config := &AppConfig{
		Store: NewStoreOptions(),
		Redis: options.NewRedisOptions(),
		MySQL: options.NewMySQLOptions(),
	}

	// Override store to use Redis with app's Redis configuration
	config.Store.Type = "redis"
	config.Store.Redis = config.Redis

	// Create store instance
	store, err := NewStore(config.Store)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Store created with type: %s\n", config.Store.Type)
	_ = store
}

// Example 4: Using different stores based on environment
func ExampleEnvironmentBasedStore() (store.Store, error) {
	env := getEnvironment() // e.g., "development", "production"

	storeOpts := NewStoreOptions()

	switch env {
	case "development":
		// Use in-memory store for development
		storeOpts.Type = "memory"

	case "production":
		// Use Redis for production
		storeOpts.Type = "redis"
		// Load Redis configuration from environment or config file
		storeOpts.Redis.Addr = getEnvOrDefault("REDIS_ADDR", "localhost:6379")
		storeOpts.Redis.Password = getEnvOrDefault("REDIS_PASSWORD", "")

		// Validate configuration
		if err := storeOpts.Redis.Validate(); err != nil {
			return nil, agentErrors.Wrap(err, agentErrors.CodeInvalidConfig, "invalid redis configuration").
				WithComponent("store_adapter_examples").
				WithOperation("example_production_setup")
		}

	default:
		// Use PostgreSQL for other environments
		storeOpts.Type = "postgres"
		storeOpts.Postgres = options.NewPostgresOptions()
		storeOpts.Postgres.Host = getEnvOrDefault("DB_HOST", "localhost")
		storeOpts.Postgres.Port = 5432
		storeOpts.Postgres.User = getEnvOrDefault("DB_USER", "postgres")
		storeOpts.Postgres.Password = getEnvOrDefault("DB_PASSWORD", "")
		storeOpts.Postgres.Database = getEnvOrDefault("DB_NAME", "myapp")
		storeOpts.Postgres.SSLMode = "prefer"
	}

	return NewStore(storeOpts)
}

// Helper functions for examples
func getEnvironment() string {
	// Implementation would read from environment variable
	return "development"
}

func getEnvOrDefault(key, defaultValue string) string {
	// Implementation would read from environment
	return defaultValue
}
