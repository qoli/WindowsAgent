//go:build !windows || !amd64

package frametap

import "errors"

func CreatePublisher(string) (Publisher, error) {
	return nil, errors.New("frame tap requires Windows amd64")
}
func OpenReader(string) (Reader, error) { return nil, errors.New("frame tap requires Windows amd64") }
