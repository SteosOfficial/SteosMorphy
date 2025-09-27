package main

import (
// #include <stdlib.h>	
    "C"
    "unsafe"
	"encoding/json"
    "github.com/steosofficial/steosmorphy/analyzer"
)
var morphAnalyzer *analyzer.MorphAnalyzer

//export CreateAnalyzer
func CreateAnalyzer() {
    morphAnalyzer, _ = analyzer.LoadMorphAnalyzer()
}

//export AnalyzeWord
func AnalyzeWord(word *C.char) *C.char {
    goWord := C.GoString(word)

    parses, forms := morphAnalyzer.Analyze(goWord)
	parsesJson, _ := json.Marshal(parses)
	formsJson, _ := json.Marshal(forms)

    var result string
	result += string(parsesJson) + " " + string(formsJson)
    
    return C.CString(result)
}

//export FreeString
func FreeString(str *C.char) {
    if str != nil {
        C.free(unsafe.Pointer(str))
    }
}

//export ReleaseAnalyzer
func ReleaseAnalyzer() {
	morphAnalyzer = nil
}

func main() {}
