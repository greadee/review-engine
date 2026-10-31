package drain

import "io"

func Close(c io.Closer) error {
	return c.Close()
}
