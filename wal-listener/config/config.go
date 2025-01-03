package config

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/asaskevich/govalidator"
	"github.com/spf13/viper"
)

type PublisherType string

const (
	PublisherTypeNats PublisherType = "nats"

// PublisherTypeKafka        PublisherType = "kafka"
// PublisherTypeRabbitMQ     PublisherType = "rabbitmq"
// PublisherTypeGooglePubSub PublisherType = "google_pubsub"
)

// Config for wal-listener.
type Config struct {
	Listener  *ListenerCfg  `valid:"required"`
	Database  *DatabaseCfg  `valid:"required"`
	Publisher *PublisherCfg `valid:"required"`
}

// ListenerCfg path of the listener config.
type ListenerCfg struct {
	SlotName          string `valid:"required"`
	AckTimeout        time.Duration
	RefreshConnection time.Duration `valid:"required"`
	HeartbeatInterval time.Duration `valid:"required"`
	Filter            FilterStruct
	TopicsMap         map[string]string
}

// PublisherCfg represent configuration for any types publisher.
type PublisherCfg struct {
	Type            PublisherType `valid:"required"`
	Address         string
	Topic           string `valid:"required"`
	TopicPrefix     string
	EnableTLS       bool   `json:"enable_tls"`
	ClientCert      string `json:"client_cert"`
	ClientKey       string `json:"client_key"`
	CACert          string `json:"ca_cert"`
	PubSubProjectID string `json:"pubsub_project_id"`
}

// DatabaseCfg path of the PostgreSQL DB config.
type DatabaseCfg struct {
	Host     string `valid:"required"`
	Port     uint16 `valid:"required"`
	Name     string `valid:"required"`
	User     string `valid:"required"`
	Password string `valid:"required"`
}

// FilterStruct incoming WAL message filter.
type FilterStruct struct {
	Tables map[string][]string
}

// Validate config data.
func (c Config) Validate() error {
	_, err := govalidator.ValidateStruct(c)
	return err
}

// InitConfig load config from file.
func InitConfig(path0 string) (*Config, error) {

	path, _ := filepath.Abs(path0)

	var conf Config

	viper.SetConfigFile(path)

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config: %w", err)
	}

	if err := viper.Unmarshal(&conf); err != nil {
		return nil, fmt.Errorf("unable to decode into config struct: %w", err)
	}

	return &conf, nil
}
