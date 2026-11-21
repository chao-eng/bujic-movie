package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/bujic-movie/bujic-movie/internal/service/cardigann"
)

func main() {
	dir := os.Args[1]
	entries, err := os.ReadDir(dir)
	if err != nil {
		fmt.Println("read:", err)
		os.Exit(1)
	}
	counts := map[string]int{}
	var partial []string
	var invalid []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yml" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		res, err := cardigann.Parse(e.Name(), raw)
		if err != nil {
			counts["error"]++
			invalid = append(invalid, e.Name()+": "+err.Error())
			continue
		}
		counts[res.Compat]++
		if res.Compat == "partial" {
			partial = append(partial, e.Name()+" -> "+fmt.Sprint(res.Detail))
		}
		if res.Compat == "invalid" {
			invalid = append(invalid, e.Name()+": "+fmt.Sprint(res.Detail))
		}
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s=%d\n", k, counts[k])
	}
	fmt.Println("--- partial sample ---")
	for i, p := range partial {
		if i >= 30 {
			break
		}
		fmt.Println(p)
	}
	fmt.Println("--- invalid ---")
	for i, p := range invalid {
		if i >= 10 {
			break
		}
		fmt.Println(p)
	}
}
