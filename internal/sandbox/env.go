package sandbox

import "os"

func envFirst(keys ...string) string {
	for _, key := range keys {
		if val, ok := os.LookupEnv(key); ok && val != "" {
			return val
		}
	}
	return ""
}

func envIsOne(key string) bool {
	return os.Getenv(key) == "1"
}

func envDefaultTrue(keys ...string) bool {
	for _, key := range keys {
		if val, ok := os.LookupEnv(key); ok {
			return val != "0"
		}
	}
	return true
}
