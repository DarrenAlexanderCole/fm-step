package main

import (
	"bytes"
)

type bufWC struct{ bytes.Buffer }

func (b *bufWC) Close() error { return nil }
