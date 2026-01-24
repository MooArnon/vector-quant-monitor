package config

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

type AppConfig struct {
	Database DatabaseConfig
	Worker   WorkerConfig
}

type AwsSecretData struct {
	TRADING_BOT_DB_POSTGRESQL_HOST     string `json:"TRADING_BOT_DB_POSTGRESQL_HOST"`
	TRADING_BOT_DB_POSTGRESQL_PASSWORD string `json:"TRADING_BOT_DB_POSTGRESQL_PASSWORD"`
	BinanceApiKey                      string `json:"BINANCE_API_KEY"`
	BinanceApiSecret                   string `json:"BINANCE_SECRET_KEY"`
	OPENAI_API_KEY                     string `json:"OPENAI_API_KEY"`
}

type WorkerConfig struct {
	HostMetricIntervalSeconds int
}

type DatabaseConfig struct {
	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string
}

func LoadConfig() *AppConfig {
	// 1. Initialize the base config with Env vars (fallbacks or non-secret values)
	cfg := &AppConfig{
		Database: DatabaseConfig{
			DBHost:     getEnv("DB_HOST", ""),
			DBPort:     getEnvAsInt("DB_PORT", 5432),
			DBUser:     getEnv("DB_USER", ""),
			DBPassword: getEnv("DB_PASSWORD", ""), // Will be overwritten
			DBName:     getEnv("DB_NAME", ""),
		},
	}

	// 2. Fetch Secrets from AWS to overwrite sensitive fields
	secretName := os.Getenv("AWS_SECRET_NAME")
	if secretName != "" {
		secrets := fetchAwsSecrets(secretName)

		// Overwrite fields if the secret value exists
		if secrets.TRADING_BOT_DB_POSTGRESQL_HOST != "" {
			cfg.Database.DBHost = secrets.TRADING_BOT_DB_POSTGRESQL_HOST
		}
		if secrets.TRADING_BOT_DB_POSTGRESQL_PASSWORD != "" {
			cfg.Database.DBPassword = secrets.TRADING_BOT_DB_POSTGRESQL_PASSWORD
		}
	} else {
		log.Println("Warning: AWS_SECRET_NAME not set. Using environment variables only.")
	}

	return cfg
}

func fetchAwsSecrets(secretName string) AwsSecretData {
	// Load the default AWS config (credentials, region from env/profile)
	awsCfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("Unable to load SDK config: %v", err)
	}

	// Create Secrets Manager client
	svc := secretsmanager.NewFromConfig(awsCfg)

	// Get the secret value
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	}

	result, err := svc.GetSecretValue(context.TODO(), input)
	if err != nil {
		log.Fatalf("Failed to retrieve secret '%s': %v", secretName, err)
	}

	// Parse JSON
	var secretData AwsSecretData
	if result.SecretString != nil {
		err = json.Unmarshal([]byte(*result.SecretString), &secretData)
		if err != nil {
			log.Fatalf("Failed to unmarshal secret JSON: %v", err)
		}
	}

	return secretData
}

func getEnv(key string, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	if valueStr, exists := os.LookupEnv(key); exists {
		if value, err := strconv.Atoi(valueStr); err == nil {
			return value
		}
	}
	return fallback
}
