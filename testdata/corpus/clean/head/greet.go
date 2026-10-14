package greet

func Greet(name string) string {
	if name == "" {
		return "hello"
	}
	return "hello " + name
}
