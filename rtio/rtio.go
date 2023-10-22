package rtio

import (
	"github.com/guowenhe/rtio-device-sdk-go/rtio/pkg/trace"
)

// 0: no trace, 1: trace critical error 2: trace details
func SetTraceLevel(level uint32) {
	if level > 2 {
		level = 2
	}
	trace.SetLevel(level)
}
