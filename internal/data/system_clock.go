package data

import (
	"time"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

type systemClock struct{}

func NewSystemClock() biz.Clock {
	return systemClock{}
}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

var _ biz.Clock = systemClock{}
