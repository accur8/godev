package publisher

import (
	"fmt"
	"strings"
	"time"

	"accur8.io/godev/wal-listener/config"
	"github.com/google/uuid"
)

// Event structure for publishing to the NATS server.
type Event struct {
	ID        uuid.UUID      `json:"id"`
	Schema    string         `json:"schema"`
	Table     string         `json:"table"`
	Action    string         `json:"action"`
	Data      map[string]any `json:"data"`
	DataOld   map[string]any `json:"dataOld"`
	EventTime time.Time      `json:"commitTime"`
}

// SubjectName creates subject name from the prefix, schema and table name. Also using topic map from cfg.
func (e *Event) SubjectName(cfg *config.Config) string {

	primaryKeySuffix := ""
	if uid, found := e.Data["uid"]; found {
		primaryKeySuffix = strings.ToLower(fmt.Sprintf(".%v", uid))
	}

	topic := cfg.Publisher.Topic + ".nefario." + e.Table + primaryKeySuffix

	fmt.Println("topic: ", topic)

	return topic
}
