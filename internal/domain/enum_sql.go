package domain

import (
	"database/sql/driver"
	"fmt"
)

// Value writes the state to the db as its text name (PENDING etc).
func (s TransferState) Value() (driver.Value, error) {
	return s.String(), nil
}

// Scan reads the text back from db into the enum.
func (s *TransferState) Scan(src any) error {
	v, err := enumFromText(src, map[string]int{
		"PENDING": int(StatePending), "PROCESSED": int(StateProcessed), "FAILED": int(StateFailed),
	})
	if err != nil {
		return err
	}
	*s = TransferState(v)
	return nil
}

// Value writes the entry type as DEBIT/CREDIT text.
func (e EntryType) Value() (driver.Value, error) {
	return e.String(), nil
}

// Scan reads DEBIT/CREDIT text back.
func (e *EntryType) Scan(src any) error {
	v, err := enumFromText(src, map[string]int{
		"DEBIT": int(Debit), "CREDIT": int(Credit),
	})
	if err != nil {
		return err
	}
	*e = EntryType(v)
	return nil
}

// enumFromText turns the db text into the int code, errors on junk.
func enumFromText(src any, lookup map[string]int) (int, error) {
	var s string
	switch t := src.(type) {
	case string:
		s = t
	case []byte:
		s = string(t)
	default:
		return 0, fmt.Errorf("cannot scan %T into enum", src)
	}
	code, ok := lookup[s]
	if !ok {
		return 0, fmt.Errorf("unknown enum value %q", s)
	}
	return code, nil
}
