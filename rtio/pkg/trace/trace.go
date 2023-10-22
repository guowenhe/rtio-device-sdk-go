package trace

import (
	"log"
)

var traceLevel uint32 = 1 // 0: no trace, 1: key infomation error 2: trace details

func SetLevel(level uint32) {
	traceLevel = level
}

func Noticef(format string, v ...any) {
	if traceLevel > 0 {
		log.Printf(format, v...)
	}
}
func Printf(format string, v ...any) {
	if traceLevel > 1 {
		log.Printf(format, v...)
	}
}
