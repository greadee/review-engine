package io

import "io"

func Drain(r io.Reader) []byte {
	data, _ := io.ReadAll(r) // revu-seed: Reliability P2 swallowed-error
	return data
}
