package sandbox

import (
	"io"
	"os"
)

var sandboxStdout io.Writer = os.Stdout
