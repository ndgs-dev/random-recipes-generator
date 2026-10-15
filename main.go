package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const out = "random-recipes.zip"

var recipePath = regexp.MustCompile(`^data/[^/]+/recipes?/.+\.json$`)

func main() {
	version := flag.String("version", "", `version to randomize: a release ("1.21.4"), a snapshot ("25w03a") or "latest"`)
	flag.Parse()
	if *version == "" {
		flag.Usage()
		os.Exit(2)
	}

	recipes, meta := collect(*version)
	shuffle(recipes)
	write(recipes, meta, *version)
}

func collect(version string) (map[string]map[string]any, map[string]any) {
	ref := version + "-data"
	if version == "latest" {
		ref = "data"
	}
	url := "https://codeload.github.com/misode/mcmeta/tar.gz/" + ref

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	check(err)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		check(fmt.Errorf("%s: %s (mcmeta has data for 1.14.4 and newer, snapshots, and latest)", url, resp.Status))
	}

	prog := &countingReader{r: resp.Body, total: int(resp.ContentLength)}
	gz, err := gzip.NewReader(prog)
	check(err)
	tr := tar.NewReader(gz)

	recipes := map[string]map[string]any{}
	var meta map[string]any
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		check(err)
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := hdr.Name[strings.IndexByte(hdr.Name, '/')+1:]
		if name != "pack.mcmeta" && !recipePath.MatchString(name) {
			continue
		}
		body, err := io.ReadAll(tr)
		check(err)
		var doc map[string]any
		check(json.Unmarshal(body, &doc))
		if name == "pack.mcmeta" {
			meta = doc
		} else {
			recipes[name] = doc
		}
	}
	bar("download", prog.done, prog.total)
	fmt.Println()
	if len(recipes) == 0 || meta == nil {
		check(fmt.Errorf("%s: the data pack has no recipes", url))
	}
	return recipes, meta
}

func shuffle(recipes map[string]map[string]any) {
	groups := map[string][]map[string]any{}
	total := 0
	for _, doc := range recipes {
		var kind string
		switch result := doc["result"].(type) {
		case string:
			kind = "string"
		case map[string]any:
			_, hasID := result["id"]
			_, hasItem := result["item"]
			if !hasID && !hasItem {
				continue
			}
			kind = "object"
		default:
			continue
		}
		groups[kind] = append(groups[kind], doc)
		total++
	}
	done := 0
	for _, docs := range groups {
		results := make([]any, len(docs))
		for i, doc := range docs {
			results[i] = doc["result"]
		}
		rand.Shuffle(len(results), func(i, j int) { results[i], results[j] = results[j], results[i] })
		for i, doc := range docs {
			doc["result"] = results[i]
			done++
			bar("shuffle", done, total)
		}
	}
	if total > 0 {
		fmt.Println()
	}
}

func write(recipes map[string]map[string]any, meta map[string]any, version string) {
	pack, ok := meta["pack"].(map[string]any)
	if !ok {
		check(fmt.Errorf("the version's pack.mcmeta has no pack object"))
	}
	pack["description"] = "Randomized recipes for Minecraft " + version

	f, err := os.Create(out)
	check(err)
	zw := zip.NewWriter(f)
	entries, done := len(recipes)+1, 0
	add(zw, "pack.mcmeta", meta)
	done++
	bar("write", done, entries)
	for name, doc := range recipes {
		add(zw, name, doc)
		done++
		bar("write", done, entries)
	}
	fmt.Println()
	check(zw.Close())
	check(f.Close())
}

func add(zw *zip.Writer, name string, doc any) {
	body, err := json.MarshalIndent(doc, "", "  ")
	check(err)
	w, err := zw.Create(name)
	check(err)
	_, err = w.Write(append(body, '\n'))
	check(err)
}

type countingReader struct {
	r     io.Reader
	total int
	done  int
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.done += n
	bar("download", c.done, c.total)
	return n, err
}

func bar(label string, done, total int) {
	const width = 20
	if total <= 0 {
		fmt.Printf("\r%-8s %d", label, done)
		return
	}
	if done > total {
		done = total
	}
	filled := done * width / total
	fmt.Printf("\r%-8s [%s%s] %3d%%", label, strings.Repeat("#", filled), strings.Repeat("-", width-filled), done*100/total)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
