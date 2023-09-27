package main

import (
	"fmt"
)

type subArg struct {
	A int
}

type Args struct {
	A int    `argparser:"arg" argparser_help:"asdasdsadasdasdsdadas"`
	Z string `argparser:"arg" argparser_help:"zxc"`
	B int    `argparser:"option,-b,--b-flag,required"`
	// C []string `argparser:"option,-c,nargs=2" argparser_default:"[1,2]"`
	// Json subArg         `argparser:"option,-json" argparser_default:"{\"a\": 666}"`
	// Map map[string]int `argparser:"option,-map" argparser_default:"{\"a\": 123}"`
	Map map[string]int `argparser:"option,-map"`
}

func main() {
	v := new(Args)
	if err := Unmarshal(v); err != nil {
		panic(err)
	}
	fmt.Println(v)
}
