// Package config package
package config

import (
	"log"
	"strings"

	"github.com/spf13/viper"

	"process_service/internal/bus"
	"process_service/internal/dlq"
	"process_service/internal/metrics"
	"process_service/internal/repository"
)

const (
	// DefaultConfigName is a default config file name without extension
	DefaultConfigName = "config"
	// DefaultConfigType is a default config filr content type
	DefaultConfigType = "yml"
)

// AppConfig is an example for app's config container
type AppConfig struct {
	RedisConf *bus.Config `mapstructure:"bus"`
	MinIOConf *dlq.Config `mapstructure:"minio"`
	RepoConf  *repository.Config `mapstructure:"repository"`
	Metrics *metrics.Config    `mapstructure:"metrics"`
}

func defaultSearchParths() []string {
	return []string{".", "../..", "~/etc", "/etc"}
}

// GetConf reads, parses config file and returns *AppConfig or error
// currently, you can not set any options to init config
func GetConf() (*AppConfig, error) {
	for _, path := range defaultSearchParths() {
		viper.AddConfigPath(path)
	}

	viper.SetConfigName(DefaultConfigName)
	viper.SetConfigType(DefaultConfigType)

	config := new(AppConfig)

	err := viper.ReadInConfig()
	if err != nil {
		log.Printf("read config failed: '%s'", err)
		return nil, err
	}

	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	err = viper.Unmarshal(&config)
	if err != nil {
		log.Printf("unmarshal failed: '%s'", err)
		return nil, err
	}

	return config, nil
}
