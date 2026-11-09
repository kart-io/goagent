// 本程序仅输出编译时版本和宿主传入的冻结提示词，不执行外部副作用。
package main

import (
	"fmt"
	"os"
)

var version = "未指定"

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "需要一个冻结提示词参数")
		os.Exit(2)
	}
	fmt.Printf("program-version=%s prompt=%s\n", version, os.Args[1])
}
