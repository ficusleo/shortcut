package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"time"
)

func (c *Client) WriteLog(entry map[string]any) error {
	return c.postLogsOrMetricsWithRetries(c.ctx, "logs", entry)
}

type LogRequest struct {
	TS  time.Time `db:"ts"`
	Val string    `db:"val"`
}

func (c *Client) postLogsOrMetricsWithRetries(ctx context.Context, table string, data map[string]any) error {
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
