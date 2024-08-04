package testutils

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func LoadEnv() {
	a, err := filepath.Abs(".")
	if err != nil {
		panic(err)
	}
	if err := loadEnvFrom(a); err != nil {
		panic(err)
	}
}

func loadEnvFrom(src string) error {
	if filepath.Dir(src) == src {
		return nil
	}

	envFile := filepath.Join(src, ".env")
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		return loadEnvFrom(filepath.Dir(src))
	} else if err != nil {
		return err
	}

	f, err := os.Open(envFile)
	if err != nil {
		return err
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if strings.HasPrefix(line, "#") || len(line) == 0 {
			continue
		}

		k, v, _ := strings.Cut(line, "=")
		if err := os.Setenv(k, v); err != nil {
			return err
		}
	}
	if s.Err() != nil {
		return s.Err()
	}
	return nil
}
