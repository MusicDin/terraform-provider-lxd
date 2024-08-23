package acctest

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strings"

	petname "github.com/dustinkirkland/golang-petname"
)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// generateString generates a random string of the given length.
func generateString(length int) string {
	s := make([]byte, length)
	for i := range s {
		s[i] = charset[rand.IntN(len(charset))]
	}
	return string(s)
}

// GenerateName generates a petname with a random string suffix.
// If requested number of words is 1 or less, just petname is returned.
func GenerateName(words int, separator string) string {
	if words <= 1 {
		return petname.Name()
	}

	return petname.Generate(words-1, separator) + separator + generateString(6)
}

// QuoteStrings converts slice of strings into a single string where each slice
// element is quoted and delimited with a comma and whitespace.
func QuoteStrings(slice []string) string {
	quoted := make([]string, len(slice))
	for i, s := range slice {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(quoted, ", ")
}

// ResetLXDEnvVars unsets all environment variables that are supported by
// the provider.
func ResetLXDEnvVars() {
	os.Unsetenv("LXD_REMOTE")
	os.Unsetenv("LXD_SCHEME")
	os.Unsetenv("LXD_ADDR")
	os.Unsetenv("LXD_PORT")
	os.Unsetenv("LXD_PASSWORD")
	os.Unsetenv("LXD_TOKEN")
}
