package system

import (
	"time"

	"github.com/google/uuid"
)

// Clock returns the current UTC time.
type Clock struct{}

func (Clock) Now() time.Time { return time.Now().UTC() }

// IDs generates UUID strings.
type IDs struct{}

func (IDs) NewID() string { return uuid.NewString() }
