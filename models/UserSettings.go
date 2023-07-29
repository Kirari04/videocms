package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
)

type UserSettings struct {
	WebhooksEnabled bool
	WebhooksMax     int
}

// Scan scan value into Jsonb, implements sql.Scanner interface
func (j *UserSettings) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New(fmt.Sprint("Failed to unmarshal JSONB value:", value))
	}

	result := UserSettings{}
	err := json.Unmarshal(bytes, &result)
	*j = UserSettings(result)
	return err
}

// Value return json value, implement driver.Valuer interface
func (j UserSettings) Value() (driver.Value, error) {
	v, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(v).MarshalJSON()
}
