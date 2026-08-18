package entity

import (
	"database/sql/driver"
	"errors"
	"fmt"
)

// JSON maps a Postgres jsonb column without pulling in a datatypes package.
// The zero value marshals as null, so a column declared not null still needs
// a default set before insert.
type JSON []byte

func (j JSON) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return string(j), nil
}

func (j *JSON) Scan(value any) error {
	if value == nil {
		*j = nil
		return nil
	}

	switch v := value.(type) {
	case []byte:
		*j = append((*j)[0:0], v...)
	case string:
		*j = JSON(v)
	default:
		return errors.New(fmt.Sprint("failed to scan JSON value:", value))
	}

	return nil
}

func (j JSON) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

func (j *JSON) UnmarshalJSON(b []byte) error {
	if j == nil {
		return errors.New("entity.JSON: UnmarshalJSON on nil pointer")
	}
	*j = append((*j)[0:0], b...)
	return nil
}

func (JSON) GormDataType() string {
	return "jsonb"
}

// JSONOrEmpty keeps not-null jsonb columns valid when no value was supplied.
func JSONOrEmpty(j JSON, fallback string) JSON {
	if len(j) == 0 {
		return JSON(fallback)
	}
	return j
}
