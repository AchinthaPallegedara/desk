// Package models holds GORM structs that MIRROR the SQL schema in /migrations.
// They never define the schema (no AutoMigrate).
package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// JSONB stores any JSON-serialisable value in a jsonb column.
type JSONB[T any] struct {
	V T
}

func NewJSONB[T any](v T) JSONB[T] { return JSONB[T]{V: v} }

func (j JSONB[T]) Value() (driver.Value, error) {
	b, err := json.Marshal(j.V)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (j *JSONB[T]) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		var zero T
		j.V = zero
		return nil
	case []byte:
		return json.Unmarshal(v, &j.V)
	case string:
		return json.Unmarshal([]byte(v), &j.V)
	default:
		return fmt.Errorf("JSONB: unsupported scan type %T", src)
	}
}

func (j JSONB[T]) MarshalJSON() ([]byte, error) { return json.Marshal(j.V) }
func (j *JSONB[T]) UnmarshalJSON(b []byte) error { return json.Unmarshal(b, &j.V) }

// Date is a calendar date (Postgres DATE), serialised as "YYYY-MM-DD".
type Date struct {
	time.Time
}

const dateLayout = "2006-01-02"

func NewDate(t time.Time) Date {
	y, m, d := t.Date()
	return Date{time.Date(y, m, d, 0, 0, 0, 0, time.UTC)}
}

func Today() Date { return NewDate(time.Now()) }

func ParseDate(s string) (Date, error) {
	t, err := time.Parse(dateLayout, strings.TrimSpace(s))
	if err != nil {
		return Date{}, fmt.Errorf("invalid date %q, expected YYYY-MM-DD", s)
	}
	return Date{t}, nil
}

func (d Date) String() string      { return d.Format(dateLayout) }
func (d Date) AddDays(n int) Date  { return Date{d.Time.AddDate(0, 0, n)} }
func (d Date) Before(o Date) bool  { return d.Time.Before(o.Time) }

func (d Date) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	p, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = p
	return nil
}

func (d Date) Value() (driver.Value, error) { return d.String(), nil }

func (d *Date) Scan(src any) error {
	switch v := src.(type) {
	case time.Time:
		*d = NewDate(v)
		return nil
	case string:
		p, err := ParseDate(v)
		*d = p
		return err
	case []byte:
		p, err := ParseDate(string(v))
		*d = p
		return err
	case nil:
		return errors.New("Date: cannot scan NULL; use *Date")
	default:
		return fmt.Errorf("Date: unsupported scan type %T", src)
	}
}
