package helper

import "fmt"

// unusedConst is never referenced anywhere.
const unusedConst = "dead constant"

// UsedConst is exported but never referenced.
const UsedConst = 42

// unusedVar is never referenced anywhere.
var unusedVar = "dead variable"

// UnusedType is a type that is never used.
type UnusedType struct {
	Field string
}

// UsedFunc is called from main — should be reachable.
func UsedFunc() {
	fmt.Println("I am used")
	internalUsed()
}

func internalUsed() {
	fmt.Println("also used")
}

func unusedHelper() {
	fmt.Println("I am dead code")
}

// UnusedExported is exported but never called — MEDIUM confidence.
func UnusedExported() {
	fmt.Println("exported but unused")
}

// github.com/go-fynx/deadcode:keep - this function should be kept
func keptByAnnotation() {
	fmt.Println("I should be kept")
}
