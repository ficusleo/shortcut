package repository

import (
	"context"
	"crypto/tls"
	"sync"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	log "github.com/sirupsen/logrus"

	"process_service/internal/metrics"
)

type Config struct {
	DSN        string        `mapstructure:"dsn"`
	User       string        `mapstructure:"user"`
	Password   string        `mapstructure:"password"`
	NumRetries int           `mapstructure:"num_retries"`
	Timeout    time.Duration `mapstructure:"timeout"`
	UseTLS     bool          `mapstructure:"use_tls"`
}

type Client struct {
	conf *Config
	conn ch.Conn
	ctx  context.Context
}

func NewClient(ctx context.Context, conf *Config) (*Client, error) {
	var tlsConfig *tls.Config
	if conf.UseTLS {
		tlsConfig = &tls.Config{InsecureSkipVerify: true}
	}

	user := conf.User
	if user == "" {
		user = "default"
	}

	c, err := ch.Open(&ch.Options{
		Protocol: ch.HTTP,
		TLS:      tlsConfig,
		Addr:     []string{conf.DSN},
		Auth: ch.Auth{
			Database: "default",
			Username: user,
			Password: conf.Password,
		},
		DialTimeout: conf.Timeout,
	})
	if err != nil {
		return nil, err
	}
	return &Client{
		conf: conf,
		conn: c,
		ctx:  ctx,
	}, nil
}

type Service struct {
	Client     *Client
	metricsSrv *metrics.Service
	ErrCh      chan error
	logger     log.Logger
	mux        *sync.Mutex
	storage    map[string]struct{}
}

func NewService(ctx context.Context, conf *Config, m *metrics.Service, errCh chan error) (*Service, error) {
	c, err := NewClient(ctx, conf)
	if err != nil {
		return nil, err
	}
	// ensure ClickHouse tables exist when running against HTTP ClickHouse
	if err := c.ensureTables(); err != nil {
		return nil, err
	}
	return &Service{
		Client:     c,
		metricsSrv: m,
		ErrCh:      errCh,
		mux:        &sync.Mutex{},
		storage:    make(map[string]struct{}),
	}, nil
}

func (s *Service) AddNotProcessedTask(taskID string) {
	s.mux.Lock()
	defer s.mux.Unlock()
	s.storage[taskID] = struct{}{}
}

func (s *Service) GetAllNotProcessedTasks() []string {
	s.mux.Lock()
	defer s.mux.Unlock()

	res := make([]string, 0, len(s.storage))
	for id := range s.storage {
		res = append(res, id)
	}

	return res
}

func (c *Client) ensureTables() error {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	ddls := []string{
		`CREATE TABLE IF NOT EXISTS logs (ts DateTime64(9), val String) ENGINE = MergeTree() ORDER BY ts`,
		`CREATE TABLE IF NOT EXISTS metrics (ts DateTime64(9), val String) ENGINE = MergeTree() ORDER BY ts`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id UUID,
			status String,
			payload String,
			failed_payload Nullable(String),
			ts DateTime64(9)
		) ENGINE = MergeTree() ORDER BY ts`,
	}
	for _, ddl := range ddls {
		if err := c.conn.Exec(ctx, ddl); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) Start() {
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.Client.ctx.Done():
				return
			case <-ticker.C:
				if s.metricsSrv != nil {
					m := s.metricsSrv.Recorder.GetMetrics()
					err := s.Client.WriteMetrics(m)
					if err != nil {
						s.ErrCh <- err
					}
				}
			}
		}
	}()
}
