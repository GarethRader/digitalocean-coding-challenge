package utils

import (
	"log"
)

func Log(level string, args ...any) {
	log.Println(append([]any{"[" + level + "]"}, args...)...)
}

func LogDebug(args ...any) {
	Log("DEBUG", args...)
}

func LogInfo(args ...any) {
	Log("INFO", args...)
}

func LogWarn(args ...any) {
	Log("WARN", args...)
}

func LogError(args ...any) {
	Log("ERROR", args...)
}
