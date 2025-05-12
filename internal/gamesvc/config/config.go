package config

import (
	"os"
)

type Config struct {
	DBUrl string
}

func Load() Config {
	return Config{
		DBUrl: os.Getenv("DATABASE_URL"), // expected to be like: postgres://user:pass@localhost:5432/dbname
	}
}
