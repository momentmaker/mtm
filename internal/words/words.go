// Package words formats counts for messages.
package words

import "fmt"

// Count returns "1 one" for a single item and "n many" otherwise.
func Count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
