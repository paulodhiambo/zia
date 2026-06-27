package phoneutil

import (
	"fmt"
	"regexp"
	"strings"
)

var nonDigits = regexp.MustCompile(`[^\d]`)

func Normalize(phone string) (string, error) {
	cleaned := nonDigits.ReplaceAllString(phone, "")

	if strings.HasPrefix(cleaned, "254") && len(cleaned) == 12 {
		return cleaned, nil
	}
	if strings.HasPrefix(cleaned, "0") && len(cleaned) == 10 {
		return "254" + cleaned[1:], nil
	}
	if strings.HasPrefix(cleaned, "7") && len(cleaned) == 9 {
		return "254" + cleaned, nil
	}
	if strings.HasPrefix(cleaned, "1") && len(cleaned) == 9 {
		return "254" + cleaned, nil
	}
	if strings.HasPrefix(cleaned, "+") {
		return cleaned[1:], nil
	}

	return "", fmt.Errorf("invalid phone number: %s", phone)
}

var maskRepl = regexp.MustCompile(`(\d{5})(\d{4})(\d{2})`)

func Mask(phone string) string {
	n, err := Normalize(phone)
	if err != nil {
		return "****"
	}
	return maskRepl.ReplaceAllString(n, "${1}****${3}")
}
