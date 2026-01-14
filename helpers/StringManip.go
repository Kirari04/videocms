package helpers

import (
	"regexp"
	"strconv"
)

func ExtractNumber(input string) int {
	re := regexp.MustCompile(`[-]?\d[\d,]*[\.]?[\d{2}]*`)
	submatchall := re.FindAllString(input, -1)
	if len(submatchall) > 0 {
		if i, err := strconv.Atoi(submatchall[0]); err == nil {
			return i
		}
	}
	return 0
}
