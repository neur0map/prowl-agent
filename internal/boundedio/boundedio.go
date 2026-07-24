// Package boundedio provides rooted descriptor opens for deadline-sensitive local reads.
package boundedio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrNonRegular reports that a bounded input resolved to a special file.
var ErrNonRegular = errors.New("bounded input is not a regular file")

// ErrNotDirectory reports that a traversed entry changed away from a directory.
var ErrNotDirectory = errors.New("bounded input is not a directory")

// ErrTooLarge reports that a bounded input exceeded its byte limit.
var ErrTooLarge = errors.New("bounded input exceeds byte limit")

func OpenRegular(root *os.Root, name string) (*os.File, error) {
	file, err := openReadOnlyNonblocking(root, name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("%w: %s", ErrNonRegular, name)
	}
	return file, nil
}

func OpenDirectory(root *os.Root, name string) (*os.File, error) {
	file, err := openReadOnlyNonblocking(root, name)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !info.IsDir() {
		file.Close()
		return nil, fmt.Errorf("%w: %s", ErrNotDirectory, name)
	}
	return file, nil
}

// ReadAllContext reads at most maxBytes from an already validated descriptor.
func ReadAllContext(ctx context.Context, file *os.File, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, errors.New("bounded read limit must be positive")
	}
	result := make([]byte, 0, min(maxBytes, 128*1024))
	buffer := make([]byte, 128*1024)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		count, err := file.Read(buffer)
		if count > 0 {
			if int64(len(result))+int64(count) > maxBytes {
				return nil, ErrTooLarge
			}
			result = append(result, buffer[:count]...)
		}
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
