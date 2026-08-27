package profile

import (
	"errors"
	"fmt"
	"os"
)

func closeTemporaryFile(file *os.File, operationErr error) error {
	if closeErr := file.Close(); closeErr != nil {
		return errors.Join(operationErr, fmt.Errorf("close temporary file: %w", closeErr))
	}
	return operationErr
}

func removeTemporaryFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove temporary file: %w", err)
	}
	return nil
}
