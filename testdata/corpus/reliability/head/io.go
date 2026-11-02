package drain

import "io"

func Close(c io.Closer) error {
	if err := c.Close(); err != nil {
		return nil // revu-seed: Reliability P2 swallowed-error
	}
	return nil
}
