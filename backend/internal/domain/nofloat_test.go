package domain

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNoFloatsInPurePackages enforces spec Section 11: money and quantity maths in the pure
// engine packages must use shopspring/decimal, never float32/float64.
func TestNoFloatsInPurePackages(t *testing.T) {
	pkgs := []string{"domain", "matcher", "tax", "charges", "corpaction", "normalize", "explain"}
	re := regexp.MustCompile(`\bfloat(32|64)\b`)
	var offenders []string
	for _, p := range pkgs {
		dir := filepath.Join("..", p)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // package not created yet
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			require.NoError(t, err)
			for i, line := range strings.Split(string(b), "\n") {
				if re.MatchString(line) && !strings.Contains(line, "nofloat:allow") {
					offenders = append(offenders, filepath.Join(p, e.Name())+":"+itoa(i+1)+": "+strings.TrimSpace(line))
				}
			}
		}
	}
	require.Empty(t, offenders, "float types are banned in pure engine packages:\n%s", strings.Join(offenders, "\n"))
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
