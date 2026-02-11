package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"shortcut/internal/metrics"
)

type Config struct {
	DSN        string
	NumRetries int
}

type Client struct {
	conf       *Config
	httpClient *http.Client

	logsPath    string
	metricsPath string
	baseURL     string
}

type Service struct {
	Client     *Client
	metricsSrv *metrics.Service
	ErrCh      chan error
	mux        *sync.Mutex
	storage    map[string]struct{}
}

func NewService(conf *Config, m *metrics.Service) (*Service, error) {
	c, err := NewClient(conf)
	if err != nil {
		return nil, err
	}
	return &Service{
		Client:     c,
		metricsSrv: m,
		ErrCh:      make(chan error, 1),
		mux:        &sync.Mutex{},
		storage:    make(map[string]struct{}),
	}, nil
}

func NewClient(conf *Config) (*Client, error) {
	c := &Client{conf: conf}
	// try to parse DSN as URL for HTTP ClickHouse
	// expected form: http(s)://host:8123[/]?param=val
	u, err := url.Parse(conf.DSN)
	if err != nil {
		// if DSN is not a valid URL, treat it as a file path for local JSON output
		c.logsPath = conf.DSN + "_logs.jsonl"
		c.metricsPath = conf.DSN + "_metrics.jsonl"
		return nil, err
	}
	base := *u
	base.RawQuery = ""
	c.baseURL = strings.TrimRight(base.String(), "/")
	c.httpClient = &http.Client{Timeout: 10 * time.Second}
	return c, nil
}

func (s *Service) Start(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
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

func (c *Client) writeJSONLine(path string, v any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	// include timestamp
	line := map[string]any{
		"ts":  time.Now().UTC().Format(time.RFC3339Nano),
		"val": json.RawMessage(b),
	}
	out, err := json.Marshal(line)
	if err != nil {
		return err
	}
	out = append(out, '\n')
	_, err = f.Write(out)
	return err
}

func (c *Client) WriteLog(entry map[string]any) error {
	if c.httpClient == nil {
		if c.logsPath == "" {
			return nil
		}
		return c.writeJSONLine(c.logsPath, entry)
	}

	// real ClickHouse via HTTP interface using JSONEachRow
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return c.postWithRetries("logs", b)
}

func (c *Client) WriteMetrics(metrics map[string]any) error {
	if c.httpClient == nil {
		if c.metricsPath == "" {
			return nil
		}
		return c.writeJSONLine(c.metricsPath, metrics)
	}
	b, err := json.Marshal(metrics)
	if err != nil {
		return err
	}
	return c.postWithRetries("metrics", b)
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

func (c *Client) postWithRetries(table string, jsonRow []byte) error {
	query := "INSERT INTO " + table + " FORMAT JSONEachRow"
	u := c.baseURL + "/?query=" + url.QueryEscape(query)

	body := jsonRow
	if len(body) == 0 || body[len(body)-1] != '\n' {
		body = append(body, '\n')
	}

	var lastErr error
	for i := range c.conf.NumRetries {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
		if err != nil {
			cancel()
			lastErr = err
			time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpClient.Do(req)
		cancel()
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("clickhouse http status %d", resp.StatusCode)
		time.Sleep(time.Duration(i+1) * 200 * time.Millisecond)
	}
	return lastErr
}
