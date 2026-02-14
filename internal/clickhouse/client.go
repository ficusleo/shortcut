package clickhouse

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"sync"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"shortcut/internal/metrics"
)

type Config struct {
	DSN        string        `mapstructure:"dsn"`
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

	c, err := ch.Open(&ch.Options{
		Protocol: ch.HTTP,
		TLS:      tlsConfig,
		Addr:     []string{conf.DSN},
		Auth: ch.Auth{
			Database: "default",
			Username: "default",
			Password: "",
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
	mux        *sync.Mutex
	storage    map[string]struct{}
	ErrCh      chan error
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
		mux:        &sync.Mutex{},
		storage:    make(map[string]struct{}),
		ErrCh:      errCh,
	}, nil
}

func (c *Client) ensureTables() error {
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()

	ddls := []string{
		`CREATE TABLE IF NOT EXISTS logs (ts DateTime64(9), val String) ENGINE = MergeTree() ORDER BY ts`,
		`CREATE TABLE IF NOT EXISTS metrics (ts DateTime64(9), val String) ENGINE = MergeTree() ORDER BY ts`,
	}
	for _, ddl := range ddls {
		if err := c.conn.Exec(ctx, ddl); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) Start() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	go func() {
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

func (c *Client) WriteLog(entry map[string]any) error {
	return c.postWithRetries(c.ctx, "logs", entry)
}

func (c *Client) WriteMetrics(metrics map[string]any) error {
	return c.postWithRetries(c.ctx, "metrics", metrics)
}

func (s *Service) AddNotProcessedTask(taskID string) {
	// store in in-memory set
	if s == nil {
		return
	}
	s.mux.Lock()
	s.storage[taskID] = struct{}{}
	s.mux.Unlock()

	if s.Client != nil {
		entry := map[string]any{"task_id": taskID}
		if err := s.Client.WriteLog(entry); err != nil {
			select {
			case s.ErrCh <- err:
			default:
			}
		}
	}
}

func (s *Service) GetAllNotProcessedTasks() []string {
	if s == nil {
		return nil
	}
	s.mux.Lock()
	defer s.mux.Unlock()

	tasks := make([]string, 0, len(s.storage))
	for taskID := range s.storage {
		tasks = append(tasks, taskID)
	}
	return tasks
}

func (c *Client) postWithRetries(ctx context.Context, table string, data map[string]any) error {
	query := "INSERT INTO " + table + " (ts, val) VALUES (?, ?)"
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	req := &LogRequest{
		TS:  time.Now(),
		Val: bytes.NewBuffer(b).String(),
	}
	for i := 0; i < c.conf.NumRetries; i++ {
		if err := c.conn.Exec(ctx, query, req.TS, req.Val); err != nil {
			if i == c.conf.NumRetries-1 {
				return err
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}
		return nil
	}
	return nil
}

type LogRequest struct {
	TS  time.Time `db:"ts"`
	Val string    `db:"val"`
}
