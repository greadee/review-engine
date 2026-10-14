package io

import "io"

func Drain(r io.Reader) []byte {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil
	}
	return data
}
