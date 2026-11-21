package main

import (
	"fmt"
	"os"

	"github.com/bujic-movie/bujic-movie/internal/service/cardigann"
)

func main() {
	raw, _ := os.ReadFile(os.Args[1])
	res, err := cardigann.Parse(os.Args[1], raw)
	if err != nil {
		fmt.Println("parse:", err)
		return
	}
	def := res.Definition
	cfg := map[string]string{}
	for _, f := range def.Settings {
		if f.Default != "" {
			cfg[f.Name] = f.Default.String()
		}
	}
	fmt.Println("config:", cfg)
	ctx := cardigann.NewTemplateContext("one piece", []string{"2000"}, nil, cfg)
	for i, path := range def.Search.Paths {
		req := cardigann.RequestBlock{Path: path.Path, Method: path.Method, QuerySeparator: path.QuerySeparator}
		req.Inputs.Values = map[string]string{}
		for _, k := range def.Search.Inputs.Keys {
			req.Inputs.Keys = append(req.Inputs.Keys, k)
			req.Inputs.Values[k] = def.Search.Inputs.Values[k]
		}
		for _, k := range path.Inputs.Keys {
			if _, ok := req.Inputs.Values[k]; !ok {
				req.Inputs.Keys = append(req.Inputs.Keys, k)
			}
			req.Inputs.Values[k] = path.Inputs.Values[k]
		}
		u, err := cardigann.BuildQueryURL(def.Links[0], req, ctx)
		fmt.Printf("path#%d %s\nerr=%v\n", i, u, err)
	}
}
