//go:build !windows

package assistlifecycle

import "context"

func run(context.Context, string, string) (Facts, error) {
	return Facts{}, ErrUnsupported
}
