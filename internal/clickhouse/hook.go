package clickhouse

import (
	"encoding/json"
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
    data["level"] = e.Level.String()
    data["msg"] = e.Message
    data["time"] = e.Time.UTC().Format("2006-01-02T15:04:05.999999999Z07:00")

    // ensure it marshals nicely
    var raw map[string]any
    b, err := json.Marshal(data)
    if err == nil {
        _ = json.Unmarshal(b, &raw)
    } else {
        raw = data
    }

    return h.client.WriteLog(raw)
}
