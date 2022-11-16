package main

import (
	"os"
	"strconv"
)

func env(key, defaultValue string) string {
	val, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}
	return val
}

func envInt(key string, defaultValue int) int {
	val, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}

	if i, err := strconv.Atoi(val); err == nil {
		return i
	}

	return defaultValue
}

func envBool(key string, defaultValue bool) bool {
	val, ok := os.LookupEnv(key)
	if !ok {
		return defaultValue
	}

	if b, err := strconv.ParseBool(val); err == nil {
		return b
	}

	return defaultValue
}
