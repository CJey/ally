package main

import (
	"fmt"
	"os"

	"github.com/cjey/ally"
)

func main() {
	done := ally.NewEvent()
	ally.ExitHook = func() {
		defer done.Emit()
		os.Exit(1)
	}
	ally.Ready()

	num := ally.Atomic("test")
	fmt.Printf("add 1, result is %d\n", num.Add(1))
	fmt.Printf("add 1, result is %d\n", num.Add(1))
	fmt.Printf("add 1, result is %d\n", num.Add(1))

	<-done.Yes()
}
