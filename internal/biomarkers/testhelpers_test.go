package biomarkers

import (
	"os"
	"strconv"
)

// osWriteFile is the test-package wrapper used by helper test fixtures.
var osWriteFile = os.WriteFile

// itoa is a one-line wrapper around strconv.Itoa used by helper test
// fixtures that build deterministic numeric file content.
func itoa(i int) string { return strconv.Itoa(i) }
