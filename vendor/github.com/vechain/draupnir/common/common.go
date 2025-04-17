package common

import (
	"time"
)

const (
	RetryPeriod = 500 * time.Millisecond
	Timeout     = 10 * time.Second
)
