package server

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
)

// Cache-Control för webbsidans filer.
const (
	// Fråga alltid om filen ändrats, så att en ny version syns direkt.
	cacheRevalidate = "no-cache"
	// Typsnitt och bilder ändras sällan.
	cacheLong = "public, max-age=86400"
)

// staticHandler serverar de inbäddade filerna. Inbäddade filer saknar
// ändringsdatum, så utan ETag kan webbläsaren inte cacha dem och laddar ned
// allt vid varje besök. ETag är en hash av innehållet och räknas ut en gång;
// oförändrade filer ger då 304. Bara filer som finns serveras – inga
// kataloglistningar.
func staticHandler(files fs.FS) (http.Handler, error) {
	etags := map[string]string{}
	err := fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		etags["/"+p] = `"` + hex.EncodeToString(sum[:12]) + `"`
		return nil
	})
	if err != nil {
		return nil, err
	}

	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/" {
			p = "/index.html"
		}
		etag, ok := etags[p]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", etag)
		switch path.Ext(p) {
		case ".html", ".css", ".js":
			w.Header().Set("Cache-Control", cacheRevalidate)
		default:
			w.Header().Set("Cache-Control", cacheLong)
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}
