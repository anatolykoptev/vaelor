package main

import "example.com/minirepo/util"

func main() {
	_ = util.SafeCall(func() error { return nil })
}
