package utils

import (
	"log"
	"os"
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

func LogFatal(args ...any) {
	Log("FATAL", args...)
	os.Exit(1)
}

func LogFatalf(format string, args ...any) {
	log.Printf("[FATAL] "+format, args...)
	os.Exit(1)
}
