package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Date is a calendar date (no time of day) that travels as "YYYY-MM-DD" in
// JSON and maps to a PostgreSQL `date` column. Using it avoids the timezone
// surprises you get from time.Time for things like contract start dates.
type Date struct{ time.Time }

const dateLayout = "2006-01-02"

func NewDate(y int, m time.Month, d int) Date {
	return Date{time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(d.Format(dateLayout))
}

func (d *Date) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		d.Time = time.Time{}
		return nil
	}
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		return fmt.Errorf("date must be in YYYY-MM-DD format")
	}
	d.Time = t
	return nil
}

// Scan implements sql.Scanner (database -> Go).
func (d *Date) Scan(v any) error {
	switch x := v.(type) {
	case nil:
		d.Time = time.Time{}
	case time.Time:
		d.Time = time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.UTC)
	default:
		return fmt.Errorf("cannot scan %T into Date", v)
	}
	return nil
}

// Value implements driver.Valuer (Go -> database).
func (d Date) Value() (driver.Value, error) {
	if d.IsZero() {
		return nil, nil
	}
	return d.Format(dateLayout), nil
}

// GormDataType tells GORM which column type this maps to.
func (Date) GormDataType() string { return "date" }
