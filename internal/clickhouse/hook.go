package clickhouse

import (
	"maps"

	log "github.com/sirupsen/logrus"
)

type LogHook struct {
	client *Client
}

func NewLogHook(c *Client) *LogHook {
	return &LogHook{client: c}
}

func (h *LogHook) Levels() []log.Level {
	return log.AllLevels
}

func (h *LogHook) Fire(e *log.Entry) error {
	data := make(map[string]any, len(e.Data)+3)
	maps.Copy(data, e.Data)
	data["time"] = e.Time.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")
    data["level"] = e.Level.String()
    data["message"] = e.Message
    
	return h.client.WriteLog(data)
}
