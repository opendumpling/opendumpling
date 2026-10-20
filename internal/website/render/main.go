// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

// Command render builds the OpenDumpling documentation website from the MDX
// sources in website/content and the controller's swagger description.
//
//	go run ./internal/website/render build
//	go run ./internal/website/render serve -addr :3000
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("dumpling-docs: ")

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	root := repoRoot()

	switch os.Args[1] {
	case "build":
		fs := flag.NewFlagSet("build", flag.ExitOnError)
		rootFlag := fs.String("root", root, "repository root")
		out := fs.String("out", filepath.Join(root, "website", "build"), "output directory")
		_ = fs.Parse(os.Args[2:])
		if err := buildSite(*rootFlag, *out, swaggerPath(*rootFlag)); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", *out)

	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		rootFlag := fs.String("root", root, "repository root")
		out := fs.String("out", filepath.Join(root, "website", "build"), "output directory")
		addr := fs.String("addr", ":3000", "listen address")
		_ = fs.Parse(os.Args[2:])
		if err := buildSite(*rootFlag, *out, swaggerPath(*rootFlag)); err != nil {
			log.Fatal(err)
		}
		log.Printf("serving %s on http://localhost%s/", *out, *addr)
		log.Fatal(http.ListenAndServe(*addr, fileServer(*out)))

	case "help", "-h", "--help":
		usage()

	default:
		log.Printf("unknown command %q", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Usage: dumpling-docs <command> [options]

Commands:
  build   Render the documentation to static HTML
  serve   Render and serve the documentation locally
  help    Show this message

Options:
  -root string   repository root (default: discovered from the current directory)
  -out  string   output directory (default: website/build)
  -addr string   listen address for serve (default: :3000)
`)
}

func swaggerPath(root string) string {
	return filepath.Join(root, "internal", "gen", "controller.swagger.json")
}

// repoRoot walks up from the working directory until it finds go.mod.
func repoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

// fileServer serves the rendered site, resolving extensionless URLs to the
// directory index the same way a production static host would.
func fileServer(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			files.ServeHTTP(w, r)
			return
		}
		// Redirect /docs/foo to /docs/foo/ so relative links resolve.
		if !strings.HasSuffix(r.URL.Path, "/") {
			if st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(r.URL.Path))); err == nil {
				if st.IsDir() {
					http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
					return
				}
			}
		}
		files.ServeHTTP(w, r)
	})
}
