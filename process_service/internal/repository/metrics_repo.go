package repository

func (c *Client) WriteMetrics(metrics map[string]any) error {
	return c.postLogsOrMetricsWithRetries(c.ctx, "metrics", metrics)
}
