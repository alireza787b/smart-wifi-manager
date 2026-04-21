package platform

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
)

func Touch(dir string, name string) error {
	if dir == "" {
		return errors.New("control directory is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	return file.Close()
}

func TailFile(path string, lines int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if lines <= 0 {
		lines = 200
	}

	scanner := bufio.NewScanner(file)
	buffer := make([]string, 0, lines)
	for scanner.Scan() {
		buffer = append(buffer, scanner.Text())
		if len(buffer) > lines {
			buffer = buffer[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return buffer, nil
}
