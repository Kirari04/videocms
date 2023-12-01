package helpers

func RemoveFromArray[T any](s []T, i int) []T {
	if len(s) == 0 || len(s) <= i || i < 0 {
		return s
	}
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}
