package store

import (
	"database/sql/driver"
	"encoding/json"
)

type jsonParameter struct{ value any }

func SQLJSON(v any) driver.Valuer { return jsonParameter{v} }
func (p jsonParameter) Value() (driver.Value, error) {
	b, err := json.Marshal(p.value)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}
